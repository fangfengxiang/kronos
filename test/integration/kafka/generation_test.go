//go:build kafka_integration && (linux || darwin)

package kafka_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestTextBackendPublishesAndConsumesGeneration(t *testing.T) {
	brokers := os.Getenv("KRONOS_KAFKA_TEST_BROKERS")
	topic := os.Getenv("KRONOS_KAFKA_TEST_TOPIC")
	if brokers == "" || topic == "" {
		t.Fatal("set KRONOS_KAFKA_TEST_BROKERS and a precreated KRONOS_KAFKA_TEST_TOPIC")
	}
	environment := append(os.Environ(), "KRONOS_KAFKA_SASL_USERNAME=", "KRONOS_KAFKA_SASL_PASSWORD=")
	var securityFlags []string
	if secure := os.Getenv("KRONOS_KAFKA_TEST_TLS_BROKERS"); secure != "" {
		brokers = secure
		securityFlags = []string{"-kafka-ca-file", os.Getenv("KRONOS_KAFKA_TEST_CA_FILE")}
		environment = append(environment,
			"KRONOS_KAFKA_SASL_USERNAME="+os.Getenv("KRONOS_KAFKA_TEST_SASL_USERNAME"),
			"KRONOS_KAFKA_SASL_PASSWORD="+os.Getenv("KRONOS_KAFKA_TEST_SASL_PASSWORD"),
		)
	}
	backendBinary := buildCommand(t, "textbackend")
	consumerBinary := buildCommand(t, "textbackend-events")
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	const prompt = "你好 · 世界"
	input := &textbackend.GenerateRequest{Prompt: prompt}
	requestID := "kafka-" + rand.Text()
	workerArguments := []string{"-kafka-brokers", brokers, "-kafka-topic", topic, "-kafka-group", "integration-" + rand.Text()}
	worker := exec.CommandContext(ctx, consumerBinary, append(workerArguments, securityFlags...)...)
	worker.Env = environment
	stdout, err := worker.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	consumer := startCommand(t, worker)
	matched := make(chan observedGeneration, 1)
	scanError := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var observation observedGeneration
			if err := json.Unmarshal(scanner.Bytes(), &observation); err != nil {
				scanError <- err
				return
			}
			if observation.RequestID == requestID {
				matched <- observation
				return
			}
		}
		scanError <- scanner.Err()
	}()
	startBackend := func(selectedTopic string) (*childProcess, *textbackend.Client) {
		t.Helper()
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := listener.Addr().String()
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		arguments := []string{"-grpc", address, "-kafka-brokers", brokers, "-kafka-topic", selectedTopic}
		command := exec.CommandContext(ctx, backendBinary, append(arguments, securityFlags...)...)
		command.Env = environment
		process := startCommand(t, command)
		client, err := textbackend.NewGRPCClient(ctx, address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		return process, client
	}
	backend, client := startBackend(topic)
	response, err := generateWhenReady(t, request.WithID(ctx, requestID), backend, client, input)
	if err != nil || response.GetText() != "backend: "+prompt {
		t.Fatalf("Kafka-enabled generation produced %v, %v", response, err)
	}
	select {
	case event := <-matched:
		if event.OutputUTF8Bytes != uint64(len(response.GetText())) || event.Partition != 0 || event.Offset < 0 {
			t.Fatalf("unexpected generation event: %+v", event)
		}
	case err := <-scanError:
		t.Fatalf("consumer output stopped before receiving the event: %v", err)
	case <-ctx.Done():
		t.Fatal("consumer did not emit the Kafka event")
	}
	consumer.stop(t, ctx)
	backend.stop(t, ctx)
	withoutTopic, blockedClient := startBackend("missing-" + rand.Text())
	_, err = generateWhenReady(t, ctx, withoutTopic, blockedClient, input)
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != textbackend.ErrorReason_ERROR_REASON_GENERATION_FAILED.String() || failure.Message != "text generation failed" {
		t.Fatalf("generation without Kafka acknowledgment returned %v", err)
	}
	withoutTopic.stop(t, ctx)
}

type observedGeneration struct {
	RequestID       string `json:"request_id"`
	OutputUTF8Bytes uint64 `json:"output_utf8_bytes"`
	Partition       int32  `json:"partition"`
	Offset          int64  `json:"offset"`
}

func generateWhenReady(t *testing.T, ctx context.Context, process *childProcess, client *textbackend.Client, input *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
	t.Helper()
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	for {
		callContext, cancel := context.WithTimeout(ctx, time.Second)
		response, err := client.Generate(callContext, input)
		cancel()
		var failure *rpc.Error
		if err == nil || (errors.As(err, &failure) && failure.Code != rpc.Unavailable) {
			return response, err
		}
		select {
		case <-process.done:
			t.Fatalf("TextBackend stopped before serving: %v", process.err)
		case <-ctx.Done():
			t.Fatalf("TextBackend did not serve: %v", err)
		case <-retry.C:
		}
	}
}

func buildCommand(t *testing.T, name string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), name)
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "./examples/textbackend/cmd/"+name)
	command.Dir = "../../.."
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, output)
	}
	return binary
}

type childProcess struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func startCommand(t *testing.T, command *exec.Cmd) *childProcess {
	t.Helper()
	var logs bytes.Buffer
	command.Stderr = &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := &childProcess{command: command, done: make(chan struct{})}
	go func() {
		process.err = command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-process.done
		if t.Failed() {
			t.Logf("%s stopped with %v\n%s", filepath.Base(command.Path), process.err, &logs)
		}
	})
	return process
}

func (process *childProcess) stop(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := process.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.done:
		if process.err != nil {
			t.Fatalf("process did not stop cleanly: %v", process.err)
		}
	case <-ctx.Done():
		t.Fatal("process did not stop after SIGTERM")
	}
}

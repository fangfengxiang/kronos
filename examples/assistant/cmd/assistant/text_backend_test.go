//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestAssistantCallsTextBackend(t *testing.T) {
	assistantBinary := buildAssistant(t)
	backendBinary := buildExampleProgram(t, "textbackend", "../../../textbackend/cmd/textbackend")
	addresses := localAddresses(t, 3)
	httpAddress, grpcAddress, backendAddress := addresses[0], addresses[1], addresses[2]
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	backend := startExampleChild(t, ctx, backendBinary, "-grpc", backendAddress)
	waitForProcessCondition(t, ctx, "text backend listener", func() bool {
		select {
		case <-backend.done:
			t.Fatalf("text backend stopped while starting: %v", backend.err)
		default:
		}
		var dialer net.Dialer
		connection, err := dialer.DialContext(ctx, "tcp", backendAddress)
		if err != nil {
			return false
		}
		connection.Close()
		return true
	})
	configPath := filepath.Join(t.TempDir(), "assistant.json")
	writeAssistantConfig(t, configPath, map[string]string{"http": httpAddress, "grpc": grpcAddress, "text-backend": backendAddress})
	process := startExampleChild(t, ctx, assistantBinary, "-config", configPath)
	httpEndpoint := "http://" + httpAddress
	waitForReady(t, ctx, httpEndpoint, process.done)
	httpClient, err := assistant.NewHTTPClient(ctx, httpEndpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer httpClient.Close()
	grpcClient, err := assistant.NewGRPCClient(ctx, grpcAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer grpcClient.Close()
	const prompt = "你好"
	const expected = "backend: " + prompt
	response, err := httpClient.Run(ctx, &assistant.RunRequest{Prompt: prompt})
	if err != nil || response.GetText() != expected {
		t.Fatalf("assistant unary result = %v, %v", response, err)
	}
	var streamed strings.Builder
	var fragments, completions int
	err = grpcClient.StreamRun(ctx, &assistant.StreamRunRequest{Prompt: prompt}, func(event *assistant.StreamRunResponse) error {
		if event.GetCompleted() != nil {
			completions++
		} else {
			fragments++
			streamed.WriteString(event.GetTextDelta())
		}
		return nil
	})
	if err != nil || streamed.String() != expected || fragments != 2 || completions != 1 {
		t.Fatalf("assistant stream = %q, %d fragments, %d completions, %v", streamed.String(), fragments, completions, err)
	}
	backend.terminate(t, ctx)
	_, err = httpClient.Run(ctx, &assistant.RunRequest{Prompt: prompt})
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != assistant.ErrorReasonRunFailed.String() || !probeOK(ctx, httpEndpoint+"/readyz") {
		t.Fatalf("assistant after backend stopped = %v", err)
	}
	process.terminate(t, ctx)
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("rebind %s: %v", address, err)
		}
		listener.Close()
	}
}

type exampleChild struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func startExampleChild(t *testing.T, ctx context.Context, binary string, arguments ...string) *exampleChild {
	t.Helper()
	command := exec.CommandContext(ctx, binary, arguments...)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	child := &exampleChild{command: command, done: make(chan struct{})}
	go func() {
		child.err = command.Wait()
		close(child.done)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-child.done
		if t.Failed() {
			t.Logf("%s exited %v\n%s", filepath.Base(binary), child.err, &output)
		}
	})
	return child
}

func (child *exampleChild) terminate(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := child.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-child.done:
		if child.err != nil {
			t.Fatalf("process did not stop cleanly: %v", child.err)
		}
	case <-ctx.Done():
		t.Fatal("process did not stop after SIGTERM")
	}
}

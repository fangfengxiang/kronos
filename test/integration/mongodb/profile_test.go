//go:build mongodb_integration && (linux || darwin)

package mongodb_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/mongodb"
)

type profileDocument struct {
	OutputPrefix *string `bson:"output_prefix,omitempty"`
}

func TestTextBackendReadsMongoDBProfile(t *testing.T) {
	uri := os.Getenv("KRONOS_MONGO_TEST_URI")
	if uri == "" {
		t.Fatal("set KRONOS_MONGO_TEST_URI for an isolated MongoDB")
	}
	databaseName := os.Getenv("KRONOS_MONGO_TEST_DATABASE")
	if databaseName == "" {
		databaseName = "kronos_test"
	}
	binary := buildTextBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	database, err := mongodb.Open(ctx, mongodb.Config{URI: uri, Database: databaseName})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := database.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	profiles, err := mongodb.NewCollection[profileDocument](database, "text_backend_profiles")
	if err != nil {
		t.Fatal(err)
	}
	profileID := "integration-" + rand.Text()
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := profiles.DeleteByID(cleanup, profileID); err != nil {
			t.Error(err)
		}
	})
	writePrefix := func(prefix *string) {
		t.Helper()
		if err := profiles.ReplaceByID(ctx, profileID, profileDocument{OutputPrefix: prefix}); err != nil {
			t.Fatal(err)
		}
	}
	firstPrefix := "文档前缀："
	writePrefix(&firstPrefix)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	backendCommand := func() *exec.Cmd {
		command := exec.CommandContext(ctx, binary, "-grpc", address, "-mongo-profile", profileID, "-mongo-database", databaseName)
		command.Env = append(os.Environ(), "KRONOS_TEXTBACKEND_MONGODB_URI="+uri)
		return command
	}
	command := backendCommand()
	var logs bytes.Buffer
	command.Stdout, command.Stderr = &logs, &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	processDone := make(chan struct{})
	var processError error
	go func() {
		processError = command.Wait()
		close(processDone)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-processDone
		if t.Failed() {
			t.Logf("text backend exited %v\n%s", processError, &logs)
		}
	})
	client, err := textbackend.NewGRPCClient(ctx, address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	const prompt = "你好"
	input := &textbackend.GenerateRequest{Prompt: prompt}
	waitForProfile(t, ctx, processDone, client, input, firstPrefix+prompt)
	for _, prefix := range []string{"更新 · ", ""} {
		writePrefix(&prefix)
		response, err := client.Generate(ctx, input)
		if err != nil || response.GetText() != prefix+prompt {
			t.Fatalf("response for prefix %q = %v, %v", prefix, response, err)
		}
	}
	assertMissingProfile := func() {
		t.Helper()
		_, err := client.Generate(ctx, input)
		var failure *rpc.Error
		if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != textbackend.ErrorReason_ERROR_REASON_GENERATION_FAILED.String() || failure.Message != "text generation failed" {
			t.Fatalf("invalid profile produced %v", err)
		}
	}
	writePrefix(nil)
	assertMissingProfile()
	if deleted, err := profiles.DeleteByID(ctx, profileID); err != nil || !deleted {
		t.Fatalf("delete profile = %v, %v", deleted, err)
	}
	assertMissingProfile()
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-processDone:
		if processError != nil {
			t.Fatalf("text backend did not stop cleanly: %v", processError)
		}
	case <-ctx.Done():
		t.Fatal("text backend did not stop after SIGTERM")
	}
	output, err := backendCommand().CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(output, []byte("read MongoDB text profile")) {
		t.Fatalf("startup with missing profile returned %v\n%s", err, output)
	}
	rebound, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("rebind text backend address: %v", err)
	}
	rebound.Close()
}

func waitForProfile(t *testing.T, ctx context.Context, processDone <-chan struct{}, client *textbackend.Client, input *textbackend.GenerateRequest, expected string) {
	t.Helper()
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	for {
		response, err := client.Generate(ctx, input)
		if err == nil {
			if response.GetText() != expected {
				t.Fatalf("initial profile text = %q, want %q", response.GetText(), expected)
			}
			return
		}
		select {
		case <-processDone:
			t.Fatal("text backend exited before serving its MongoDB profile")
		case <-ctx.Done():
			t.Fatalf("text backend did not serve its MongoDB profile: %v", err)
		case <-retry.C:
		}
	}
}

func buildTextBackend(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "textbackend")
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "./examples/textbackend/cmd/textbackend")
	command.Dir = "../../.."
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build text backend: %v\n%s", err, output)
	}
	return binary
}

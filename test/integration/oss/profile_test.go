//go:build linux || darwin

package oss_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestTextBackendReadsOSSProfile(t *testing.T) {
	buildContext, stopBuild := context.WithTimeout(t.Context(), 2*time.Minute)
	defer stopBuild()
	binary := filepath.Join(t.TempDir(), "textbackend")
	build := exec.CommandContext(buildContext, "go", "build", "-o", binary, "./examples/textbackend/cmd/textbackend")
	build.Dir = "../../.."
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build text backend: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const objectKey = "profiles/你好.txt"
	var object atomic.Pointer[string]
	fixture := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/fixture-bucket/"+objectKey ||
			!strings.HasPrefix(request.Header.Get("Authorization"), "OSS4-HMAC-SHA256 Credential=fixture-key/") || request.Header.Get("x-oss-security-token") != "fixture-session" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		prefix := object.Load()
		if prefix == nil {
			writer.Header().Set("Content-Type", "application/xml")
			writer.WriteHeader(http.StatusNotFound)
			io.WriteString(writer, "<Error><Code>NoSuchKey</Code></Error>")
			return
		}
		io.WriteString(writer, *prefix)
	}))
	defer fixture.Close()
	firstPrefix := "对象存储："
	object.Store(&firstPrefix)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	backendCommand := func() *exec.Cmd {
		command := exec.CommandContext(ctx, binary, "-grpc", address, "-oss-profile", objectKey)
		command.Env = append(os.Environ(),
			"KRONOS_TEXTBACKEND_OSS_REGION=cn-hangzhou", "KRONOS_TEXTBACKEND_OSS_BUCKET=fixture-bucket", "KRONOS_TEXTBACKEND_OSS_ENDPOINT="+fixture.URL,
			"OSS_ACCESS_KEY_ID=fixture-key", "OSS_ACCESS_KEY_SECRET=fixture-secret", "OSS_SESSION_TOKEN=fixture-session", "OSS_SDK_LOG_LEVEL=debug")
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
	for _, prefix := range []string{"第二版 · ", ""} {
		object.Store(&prefix)
		result, err := client.Generate(ctx, input)
		if err != nil || result.GetText() != prefix+prompt {
			t.Fatalf("response for prefix %q = %v, %v", prefix, result, err)
		}
	}
	assertProfileFailure := func() {
		t.Helper()
		_, err := client.Generate(ctx, input)
		var failure *rpc.Error
		if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != textbackend.ErrorReason_ERROR_REASON_GENERATION_FAILED.String() || failure.Message != "text generation failed" {
			t.Fatalf("invalid profile produced %v", err)
		}
	}
	for _, invalid := range []string{strings.Repeat("X", 4097), "\xff"} {
		object.Store(&invalid)
		assertProfileFailure()
	}
	object.Store(nil)
	assertProfileFailure()
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
	if bytes.Contains(logs.Bytes(), []byte("fixture-secret")) || bytes.Contains(logs.Bytes(), []byte("fixture-session")) {
		t.Fatal("text backend logged OSS credentials")
	}
	output, err := backendCommand().CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(output, []byte("open OSS text profile")) || bytes.Contains(output, []byte("fixture-secret")) {
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
			t.Fatal("text backend exited before serving its OSS profile")
		case <-ctx.Done():
			t.Fatalf("text backend did not serve its OSS profile: %v", err)
		case <-retry.C:
		}
	}
}

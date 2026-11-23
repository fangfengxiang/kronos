package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestAdmissionRejectsRequestsBeforeReady(t *testing.T) {
	service := admittingService{ready: func() bool { return false }, next: agentService{}}
	_, err := service.Run(t.Context(), &assistant.RunRequest{Prompt: "你好"})
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.Unavailable || failure.Reason != rpc.ServiceUnavailableReason {
		t.Fatalf("not-ready request returned %v", err)
	}
}

func TestAssistantProcessShutsDownOnSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the operating system does not deliver SIGTERM to child processes")
	}
	binary := buildAssistant(t)
	addresses := localAddresses(t, 4)
	httpAddress, grpcAddress, sseAddress, microAddress := addresses[0], addresses[1], addresses[2], addresses[3]
	configPath := filepath.Join(t.TempDir(), "assistant.json")
	writeAssistantConfig(t, configPath, map[string]string{
		"http":             "127.0.0.1:0",
		"grpc":             grpcAddress,
		"sse":              sseAddress,
		"micro-v2":         microAddress,
		"shutdown-timeout": "2s",
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-config", configPath, "-http", httpAddress)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var processError error
	processDone := make(chan struct{})
	go func() {
		processError = command.Wait()
		close(processDone)
	}()
	t.Cleanup(func() {
		cancel()
		<-processDone
		if t.Failed() {
			t.Logf("assistant process error: %v\n%s", processError, &output)
		}
	})

	httpEndpoint := "http://" + httpAddress
	waitForReady(t, ctx, httpEndpoint, processDone)
	if !probeOK(ctx, httpEndpoint+"/livez") {
		t.Fatal("liveness probe did not return HTTP 200")
	}
	assertAssistantListeners(t, ctx, httpAddress, grpcAddress, sseAddress, microAddress)

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-processDone:
		if processError != nil {
			t.Fatalf("assistant did not exit cleanly: %v", processError)
		}
	case <-ctx.Done():
		t.Fatal("assistant did not exit after SIGTERM")
	}
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("rebind %s: %v", address, err)
		}
		listener.Close()
	}
}

func buildAssistant(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "assistant")
	buildContext, cancelBuild := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildContext, "go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build assistant: %v\n%s", err, output)
	}
	return binary
}

func assertAssistantListeners(t *testing.T, ctx context.Context, httpAddress, grpcAddress, sseAddress, microAddress string) {
	t.Helper()
	httpClient, err := assistant.NewHTTPClient(ctx, "http://"+httpAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer httpClient.Close()
	grpcClient, err := assistant.NewRPCClient(ctx, grpcAddress, time.Second, rpc.WithProtocol(rpc.ProtocolGRPC))
	if err != nil {
		t.Fatal(err)
	}
	defer grpcClient.Close()
	assertEcho(t, ctx, "HTTP", httpClient)
	assertEcho(t, ctx, "gRPC", grpcClient)
	assertLocalizedPromptErrors(t, ctx, "HTTP", httpClient)
	assertLocalizedPromptErrors(t, ctx, "gRPC", grpcClient)
	legacyClient, err := assistant.NewRPCClient(ctx, microAddress, time.Second, rpc.WithProtocol(rpc.ProtocolMicroV2))
	if err != nil {
		t.Fatal(err)
	}
	defer legacyClient.Close()
	assertEcho(t, ctx, "go-micro v2", legacyClient)
	assertStandardGRPCStream(t, ctx, grpcAddress)
	assertSSEListener(t, ctx, sseAddress, httpAddress)
	assertMicroListener(t, ctx, microAddress)
}

func localAddresses(t *testing.T, count int) []string {
	t.Helper()
	addresses := make([]string, 0, count)
	for range count {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		addresses = append(addresses, listener.Addr().String())
	}
	return addresses
}

func assertStandardGRPCStream(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := wire.NewAssistantClient(connection)
	stream, err := client.StreamRun(ctx, &wire.StreamRunRequest{Prompt: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"你", "好"} {
		event, err := stream.Recv()
		if err != nil || event.GetTextDelta() != expected {
			t.Fatalf("gRPC delta = %v, %v", event, err)
		}
	}
	completion, err := stream.Recv()
	if err != nil || completion.GetCompleted() == nil {
		t.Fatalf("gRPC completion = %v, %v", completion, err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("gRPC stream ended with %v", err)
	}
	localizedCtx := metadata.AppendToOutgoingContext(ctx, localization.AcceptLanguageHeader, "zh-CN")
	stream, err = client.StreamRun(localizedCtx, &wire.StreamRunRequest{})
	if err == nil {
		_, err = stream.Recv()
	}
	if failure := status.Convert(err); failure.Code() != codes.InvalidArgument || failure.Message() != "提示词不能为空" {
		t.Fatalf("localized gRPC stream failure = %v", failure)
	}
}

func assertSSEListener(t *testing.T, ctx context.Context, sseAddress, httpAddress string) {
	t.Helper()
	for _, test := range []struct {
		address, path string
		status        int
	}{
		{sseAddress, "/v1/assistant:streamRun?prompt=override", http.StatusOK},
		{httpAddress, "/v1/assistant:streamRun", http.StatusNotFound},
		{sseAddress, "/v1/assistant:run", http.StatusNotFound},
	} {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+test.address+test.path, strings.NewReader(`{"prompt":"你好"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != test.status {
			t.Fatalf("%s on %s = %d %s, %v", test.path, test.address, response.StatusCode, body, err)
		}
		if test.status == http.StatusOK && (response.Header.Get("Content-Type") != "text/event-stream" ||
			bytes.Count(body, []byte("event: message")) != 3 || !bytes.Contains(body, []byte(`"completed"`)) ||
			!bytes.Contains(body, []byte(`"textDelta":"你"`)) || !bytes.Contains(body, []byte(`"textDelta":"好"`))) {
			t.Fatalf("unexpected SSE response: %v\n%s", response.Header, body)
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+sseAddress+"/v1/assistant:streamRun", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Add(localization.AcceptLanguageHeader, "en;q=0.4")
	request.Header.Add(localization.AcceptLanguageHeader, "zh-CN;q=0.9")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusBadRequest || !bytes.Contains(body, []byte("提示词不能为空")) ||
		!bytes.Contains(body, []byte(assistant.ErrorReasonInvalidPrompt.String())) {
		t.Fatalf("localized SSE response = %d %s, %v", response.StatusCode, body, err)
	}
}

func assertMicroListener(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	payload, err := proto.Marshal(&assistant.RunRequest{Prompt: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/protobuf")
	request.Header.Set("Micro-Service", "kronos.assistant.v1.Assistant")
	request.Header.Set("Micro-Endpoint", "Assistant.Run")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || response.Header.Get("Micro-Error") != "" {
		t.Fatalf("legacy RPC response = %d %v, %v", response.StatusCode, response.Header, err)
	}
	var decoded assistant.RunResponse
	if err := proto.Unmarshal(body, &decoded); err != nil || decoded.GetText() != "你好" {
		t.Fatalf("legacy response = %v, %v", &decoded, err)
	}
}

func waitForReady(t *testing.T, ctx context.Context, endpoint string, processDone <-chan struct{}) {
	t.Helper()
	retry := time.NewTicker(20 * time.Millisecond)
	defer retry.Stop()
	for !probeOK(ctx, endpoint+"/readyz") {
		select {
		case <-processDone:
			t.Fatal("assistant exited before it became ready")
		case <-ctx.Done():
			t.Fatal("assistant did not become ready")
		case <-retry.C:
		}
	}
}

func probeOK(ctx context.Context, endpoint string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func assertEcho(t *testing.T, ctx context.Context, transport string, client *assistant.Client) {
	t.Helper()
	response, err := client.Run(ctx, &assistant.RunRequest{Prompt: " 你好 "})
	if err != nil || response.GetText() != " 你好 " {
		t.Fatalf("%s assistant returned %v, %v", transport, response, err)
	}
	_, err = client.Run(ctx, &assistant.RunRequest{Prompt: " \t"})
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.InvalidArgument || failure.Reason != assistant.ErrorReasonInvalidPrompt.String() || failure.Message != "prompt must not be blank" {
		t.Fatalf("%s blank request returned %v", transport, err)
	}
}

func assertLocalizedPromptErrors(t *testing.T, ctx context.Context, transport string, client *assistant.Client) {
	t.Helper()
	for _, test := range []struct {
		preferences []string
		message     string
	}{
		{[]string{"fr-FR", "en;q=0.4, zh-CN;q=0.9"}, "提示词不能为空"},
		{[]string{"fr-FR", "zh-CN;q=0"}, "prompt must not be blank"},
	} {
		requestCtx := localization.WithAcceptLanguage(ctx, test.preferences...)
		_, err := client.Run(requestCtx, &assistant.RunRequest{})
		var failure *rpc.Error
		if !errors.As(err, &failure) || failure.Code != rpc.InvalidArgument || failure.Reason != assistant.ErrorReasonInvalidPrompt.String() || failure.Message != test.message {
			t.Fatalf("%s preferences %v returned %v", transport, test.preferences, err)
		}
	}
}

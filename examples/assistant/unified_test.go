package assistant_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/prometheus"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func TestUnifiedAssistantStreaming(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	addresses := reserveUnifiedAddresses(t)
	metrics := prometheus.NewServiceRPCMetrics()
	application, err := kronos.New(kronos.Name("kronos.assistant.v1.Assistant"),
		kronos.GRPC(addresses[0]), kronos.SSE(addresses[1]), kronos.HTTP(addresses[2]),
		kronos.MicroRPC(addresses[3]), kronos.Observability(kronos.ObservabilityConfig{
			DiagnosticsAddress: addresses[4], Metrics: metrics,
		}))
	if err != nil {
		t.Fatal(err)
	}
	const callerID = "unified-assistant.01"
	stopped := make(chan error, 1)
	drain := make(chan struct{})
	stream := testStream(func(ctx context.Context, input *assistant.StreamRunRequest, emit func(string) error) error {
		if request.ID(ctx) != callerID {
			t.Errorf("business stream request ID = %q, want %q", request.ID(ctx), callerID)
		}
		if input.GetPrompt() == "" {
			return &rpc.Error{Code: rpc.InvalidArgument, Reason: assistant.ErrorReasonInvalidPrompt.String(), Message: "prompt is blank"}
		}
		if err := emit("首段"); err != nil {
			return err
		}
		switch input.GetPrompt() {
		case "fail late":
			return errors.New("private downstream failure")
		case "cancel":
			<-ctx.Done()
			stopped <- ctx.Err()
			return ctx.Err()
		case "drain":
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-drain:
			}
		}
		return emit("次段")
	})
	if err := application.Register(assistant.BindService(&assistantService{}, stream)); err != nil {
		t.Fatal(err)
	}
	run, stop := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { result <- application.Run(run) }()
	t.Cleanup(func() {
		stop()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := await(t, shutdown, result); err != nil {
			t.Errorf("unified app shutdown: %v", err)
		}
	})
	ready, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addresses[4]+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	ticks := time.NewTicker(15 * time.Millisecond)
	defer ticks.Stop()
	for {
		response, err := http.DefaultClient.Do(ready)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		await(t, ctx, ticks.C)
	}

	grpc, err := assistant.NewRPCClient(ctx, addresses[0], time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer grpc.Close()
	call := request.WithID(ctx, callerID)
	if response, err := grpc.Run(call, &assistant.RunRequest{Prompt: "one service"}); err != nil || response.GetText() != "one service" {
		t.Fatalf("gRPC unary on a mixed service: %v, %v", response, err)
	}
	var deltas string
	err = grpc.StreamRun(call, &assistant.StreamRunRequest{Prompt: "stream"}, func(event *assistant.StreamRunResponse) error {
		deltas += event.GetTextDelta()
		return nil
	})
	if err != nil || deltas != "首段次段" {
		t.Fatalf("gRPC stream = %q, %v", deltas, err)
	}
	for _, test := range []struct {
		name, reason string
		deltas       string
	}{
		{"", assistant.ErrorReasonInvalidPrompt.String(), ""},
		{"fail late", assistant.ErrorReasonRunFailed.String(), "首段"},
	} {
		deltas = ""
		err := grpc.StreamRun(call, &assistant.StreamRunRequest{Prompt: test.name}, func(event *assistant.StreamRunResponse) error {
			deltas += event.GetTextDelta()
			return nil
		})
		var failure *rpc.Error
		if !errors.As(err, &failure) || failure.Reason != test.reason || deltas != test.deltas {
			t.Fatalf("gRPC stream %q = %q, %v", test.name, deltas, err)
		}
	}
	receiverDone := errors.New("receiver finished")
	if err := grpc.StreamRun(call, &assistant.StreamRunRequest{Prompt: "cancel"}, func(*assistant.StreamRunResponse) error {
		return receiverDone
	}); !errors.Is(err, receiverDone) {
		t.Fatalf("gRPC early stop: %v", err)
	}
	if err := await(t, ctx, stopped); !errors.Is(err, context.Canceled) {
		t.Fatalf("gRPC business cancel: %v", err)
	}

	post := func(body string) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addresses[1]+"/v1/assistant:streamRun", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(request.IDHeader, callerID)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	for _, test := range []struct {
		body, first, last string
		status            int
	}{
		{`{"prompt":"stream"}`, "首段", `"completed"`, http.StatusOK},
		{`{}`, "ERROR_REASON_INVALID_PROMPT", "", http.StatusBadRequest},
		{`{"prompt":"fail late"}`, "首段", "event: error", http.StatusOK},
	} {
		response := post(test.body)
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		body := string(data)
		if err != nil || response.StatusCode != test.status || !strings.Contains(body, test.first) ||
			!strings.Contains(body, test.last) || strings.Index(body, test.first) > strings.LastIndex(body, test.last) ||
			strings.Contains(body, "private downstream failure") {
			t.Fatalf("SSE %s = %d %q, %v", test.body, response.StatusCode, body, err)
		}
	}
	response := post(`{"prompt":"cancel"}`)
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != "event: message\n" {
		response.Body.Close()
		t.Fatalf("first SSE event = %q, %v", line, err)
	}
	response.Body.Close()
	if err := await(t, ctx, stopped); !errors.Is(err, context.Canceled) {
		t.Fatalf("SSE business cancel: %v", err)
	}
	micro, err := assistant.NewRPCClient(ctx, "http://"+addresses[3], time.Second, rpc.WithProtocol(rpc.ProtocolMicroV2))
	if err != nil {
		t.Fatal(err)
	}
	defer micro.Close()
	if reply, err := micro.Run(call, &assistant.RunRequest{Prompt: "one service"}); err != nil || reply.GetText() != "one service" {
		t.Fatalf("MicroRPC mixed service unary = %v, %v", reply, err)
	}
	unary := postAssistant(t, ctx, addresses[2], `{"prompt":"one service"}`)
	defer unary.Body.Close()
	if unary.StatusCode != http.StatusOK {
		t.Fatalf("HTTP mixed service unary = %d", unary.StatusCode)
	}
	scrape, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addresses[4]+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	sample, err := http.DefaultClient.Do(scrape)
	if err != nil {
		t.Fatal(err)
	}
	defer sample.Body.Close()
	data, err := io.ReadAll(sample.Body)
	if err != nil || sample.StatusCode != http.StatusOK {
		t.Fatalf("metrics returned %d, %v", sample.StatusCode, err)
	}
	for _, protocol := range []string{"grpc", "sse"} {
		line := `kronos_rpc_requests_total{operation="/kronos.assistant.v1.Assistant/StreamRun",outcome="success",protocol="` + protocol + `",side="server"} 1`
		if !strings.Contains(string(data), "\n"+line+"\n") {
			t.Errorf("logical stream metric missing %q", line)
		}
	}

	grpcStarted := make(chan struct{}, 1)
	grpcDrained := make(chan error, 1)
	go func() {
		grpcDrained <- grpc.StreamRun(call, &assistant.StreamRunRequest{Prompt: "drain"}, func(event *assistant.StreamRunResponse) error {
			if event.GetTextDelta() == "首段" {
				grpcStarted <- struct{}{}
			}
			return nil
		})
	}()
	await(t, ctx, grpcStarted)
	response = post(`{"prompt":"drain"}`)
	defer response.Body.Close()
	reader = bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != "event: message\n" {
		t.Fatalf("first draining SSE event = %q, %v", line, err)
	}
	stop()
	for {
		probe, err := http.DefaultClient.Do(ready)
		if err != nil {
			break
		}
		probe.Body.Close()
		if probe.StatusCode != http.StatusOK {
			break
		}
		await(t, ctx, ticks.C)
	}
	close(drain)
	if err := await(t, ctx, grpcDrained); err != nil {
		t.Fatalf("admitted gRPC stream failed to drain: %v", err)
	}
	if remaining, err := io.ReadAll(reader); err != nil || !strings.Contains(string(remaining), `"completed"`) {
		t.Fatalf("admitted SSE stream failed to drain: %q, %v", remaining, err)
	}
}

func reserveUnifiedAddresses(t *testing.T) [5]string {
	t.Helper()
	var addresses [5]string
	for index := range addresses {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		addresses[index] = listener.Addr().String()
	}
	return addresses
}

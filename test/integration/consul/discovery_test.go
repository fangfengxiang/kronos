//go:build consul_integration && (linux || darwin)

package consul_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/consul"
)

func TestAssistantDiscoversConsulBackends(t *testing.T) {
	binary := buildReferenceCommand(t, "assistant", "./examples/assistant/cmd/assistant")
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	agentEndpoint, registry := startConsulAgent(t, ctx)
	ports := loopbackPorts(t, 2)
	httpAddress := "127.0.0.1:" + strconv.Itoa(ports[0])
	grpcAddress := "127.0.0.1:" + strconv.Itoa(ports[1])
	process := startChild(t, ctx, binary, "-http", httpAddress, "-grpc", grpcAddress, "-text-backend-consul", agentEndpoint)
	httpEndpoint := "http://" + httpAddress
	await(t, ctx, "Assistant readiness", func() bool {
		select {
		case <-process.done:
			t.Fatalf("Assistant exited during startup: %v", process.err)
		default:
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpEndpoint+"/readyz", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == http.StatusOK
	})
	client, err := assistant.NewHTTPClient(ctx, httpEndpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	const prompt = "你好"
	invoke := func() (string, error) {
		callContext, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		response, err := client.Run(callContext, &assistant.RunRequest{Prompt: prompt})
		return response.GetText(), err
	}
	modelFailed := func(err error) bool {
		var failure *rpc.Error
		return errors.As(err, &failure) && failure.Code == rpc.Internal && failure.Reason == assistant.ErrorReasonRunFailed.String()
	}
	await(t, ctx, "model failure with no instances", func() bool {
		_, err := invoke()
		return modelFailed(err)
	})
	first := consul.Instance{ID: "first", Name: textbackend.DefaultServiceName, Address: startDistinctBackend(t, "first")}
	second := consul.Instance{ID: "second", Name: textbackend.DefaultServiceName, Address: startDistinctBackend(t, "second")}
	for _, instance := range []consul.Instance{first, second} {
		if err := registry.Register(ctx, instance); err != nil {
			t.Fatal(err)
		}
	}
	const firstReply = "first: " + prompt
	const secondReply = "second: " + prompt
	seen := make(map[string]bool)
	await(t, ctx, "both healthy instances", func() bool {
		if reply, err := invoke(); err == nil {
			seen[reply] = true
		}
		return seen[firstReply] && seen[secondReply]
	})
	if err := registry.Deregister(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	consecutive := 0
	await(t, ctx, "exclusive routing to the remaining instance", func() bool {
		if reply, err := invoke(); err != nil || reply != secondReply {
			consecutive = 0
			return false
		}
		consecutive++
		return consecutive >= 8
	})
	if err := registry.Deregister(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	await(t, ctx, "failure after the last instance was withdrawn", func() bool {
		_, err := invoke()
		return modelFailed(err)
	})
	for _, instance := range []consul.Instance{first, second} {
		assertWithdrawnInstanceStillServes(t, ctx, instance, prompt)
	}
	for range 4 {
		if reply, err := invoke(); !modelFailed(err) {
			t.Fatalf("Assistant routed to a withdrawn backend: %q, %v", reply, err)
		}
	}
	if err := registry.Register(ctx, second); err != nil {
		t.Fatal(err)
	}
	await(t, ctx, "routing after an instance returned", func() bool {
		reply, err := invoke()
		return err == nil && reply == secondReply
	})
	process.stop(t, ctx)
}

func startDistinctBackend(t *testing.T, label string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	server, err := textbackend.NewGRPCServer(listener, namedBackend(label))
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Start(context.WithoutCancel(t.Context())) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	return listener.Addr().String()
}

type namedBackend string

func (name namedBackend) Generate(_ context.Context, input *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
	return &textbackend.GenerateResponse{Text: string(name) + ": " + input.GetPrompt()}, nil
}

func assertWithdrawnInstanceStillServes(t *testing.T, ctx context.Context, instance consul.Instance, prompt string) {
	t.Helper()
	client, err := textbackend.NewGRPCClient(ctx, instance.Address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	response, err := client.Generate(ctx, &textbackend.GenerateRequest{Prompt: prompt})
	if err != nil || response.GetText() != instance.ID+": "+prompt {
		t.Fatalf("withdrawn instance %s stopped serving: %v, %v", instance.ID, response, err)
	}
}

func await(t *testing.T, ctx context.Context, condition string, observed func() bool) {
	t.Helper()
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	for !observed() {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", condition)
		case <-retry.C:
		}
	}
}

//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/infra/etcd"
	"github.com/fangfengxiang/kronos/infra/prometheus"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

var orderProtocols = [...]kronos.WireProtocol{kronos.ProtocolGRPC, kronos.ProtocolMicroV2}

func TestOrderWithdrawsBeforeBothProtocolsDrain(t *testing.T) {
	logger, logPath := newRPCLogCapture(t)
	registry := &withdrawalBarrier{
		Registry: openTestRegistry(t), started: make(chan struct{}), proceed: make(chan struct{}),
	}
	finishWithdrawal := sync.OnceFunc(func() { close(registry.proceed) })
	defer finishWithdrawal()
	name := "orders.drain." + rand.Text()
	implementation := newWaitingOrder()
	finishCalls := sync.OnceFunc(func() { close(implementation.release) })
	defer finishCalls()
	metrics := prometheus.NewServiceRPCMetrics()
	diagnostics := availableDiagnosticAddress(t)
	httpAddress := availableDiagnosticAddress(t)
	diagnosticClient := newDiagnosticClient(t)
	running := runOrderReplica(t, registry, name, implementation, kronos.Observability(kronos.ObservabilityConfig{
		DiagnosticsAddress: diagnostics, Metrics: prometheus.NewServiceRPCMetrics(), Logger: logger,
	}), kronos.Metrics(metrics), kronos.HTTP(httpAddress))
	waitForInstances(t, registry.Registry, name, kronos.ProtocolGRPC, 1)
	initial := diagnosticGET(t, diagnosticClient, diagnostics, "/metrics", http.StatusOK)
	for _, protocol := range orderProtocols {
		assertOrderInFlightMetric(t, initial, protocol, "server", 0)
	}
	callContext, cancelCalls := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancelCalls()

	var calls []pendingOrder
	for _, protocol := range orderProtocols {
		address := waitForInstances(t, registry.Registry, name, protocol, 1)[0]
		orders := newDiscoveredOrder(t, registry, name, protocol, kronos.Observability(kronos.ObservabilityConfig{
			Metrics: metrics, Logger: logger,
		}))
		result := beginOrderCall(callContext, orders, string(protocol))
		serverContext := awaitOrderSignal(t, implementation.entered)
		calls = append(calls, pendingOrder{orders: orders, result: result, serverContext: serverContext, address: address})
	}
	httpResult := make(chan error, 1)
	go func() {
		reply, err := drainOrderHTTP(callContext, diagnosticClient, httpAddress)
		if err == nil && (reply.code != http.StatusOK || reply.OrderID != "http-drain" || reply.Status != "after-drain") {
			err = fmt.Errorf("unexpected HTTP reply %+v", reply)
		}
		httpResult <- err
	}()
	httpContext := awaitOrderSignal(t, implementation.entered)
	diagnosticGET(t, diagnosticClient, diagnostics, "/readyz", http.StatusOK)
	sample := diagnosticGET(t, diagnosticClient, diagnostics, "/metrics", http.StatusOK)
	assertOrderInFlightMetric(t, sample, "http", "server", 1)
	for _, protocol := range orderProtocols {
		for _, side := range []string{"client", "server"} {
			assertOrderInFlightMetric(t, sample, protocol, side, 1)
		}
	}
	running.stop()
	awaitOrderSignal(t, registry.started)
	diagnosticGET(t, diagnosticClient, diagnostics, "/livez", http.StatusOK)
	diagnosticGET(t, diagnosticClient, diagnostics, "/readyz", http.StatusServiceUnavailable)
	for _, call := range calls {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		_, err := call.orders.GetOrder(ctx, &order.GetOrderRequest{OrderId: "arrived-during-withdrawal"})
		cancel()
		var failure *kronos.Error
		if !errors.As(err, &failure) || failure.Code != kronos.Unavailable || failure.Reason != "SERVICE_UNAVAILABLE" {
			t.Fatalf("listener %s during withdrawal = %v, want SERVICE_UNAVAILABLE", call.address, err)
		}
	}
	if reply, err := drainOrderHTTP(callContext, diagnosticClient, httpAddress); err != nil ||
		reply.code != http.StatusServiceUnavailable || reply.Reason != "SERVICE_UNAVAILABLE" {
		t.Fatalf("HTTP during withdrawal = %+v, %v", reply, err)
	}
	if got := implementation.calls.Load(); got != 3 {
		t.Fatalf("shared Order implementation admitted %d calls, want 3", got)
	}
	finishWithdrawal()
	for _, protocol := range orderProtocols {
		waitForInstances(t, registry.Registry, name, protocol, 0)
	}
	for _, call := range calls {
		if err := call.serverContext.Err(); err != nil {
			t.Fatalf("business call canceled before drain: %v", err)
		}
		select {
		case result := <-call.result:
			t.Fatalf("business call completed before release: %v", result.err)
		default:
		}
	}
	if err := httpContext.Err(); err != nil {
		t.Fatalf("HTTP business call canceled before drain: %v", err)
	}
	finishCalls()
	if err := awaitOrderSignal(t, httpResult); err != nil {
		t.Errorf("HTTP in-flight result: %v", err)
	}
	for i, call := range calls {
		result := awaitOrderSignal(t, call.result)
		if result.err != nil || result.response.GetOrderId() != string(orderProtocols[i]) ||
			result.response.GetStatus() != "after-drain" {
			t.Errorf("%s in-flight result = %v, %v", orderProtocols[i], result.response, result.err)
		}
	}
	if err := running.wait(t); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		assertOrderPortReleased(t, call.address)
	}
	assertOrderPortReleased(t, diagnostics)
	assertOrderPortReleased(t, httpAddress)
	scrape := httptest.NewRecorder()
	metrics.ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assertOrderInFlightMetric(t, scrape.Body.String(), "http", "server", 0)
	assertOrderOutcomeMetrics(t, scrape.Body.String(), "http", "server", 1, 1)
	for _, protocol := range orderProtocols {
		for _, side := range []string{"client", "server"} {
			assertOrderInFlightMetric(t, scrape.Body.String(), protocol, side, 0)
			assertOrderOutcomeMetrics(t, scrape.Body.String(), protocol, side, 1, 1)
		}
	}
	assertDrainRPCLogs(t, logPath, name)
}

type orderHTTPDrainReply struct {
	code    int
	OrderID string `json:"orderId"`
	Status  string
	Reason  string
}

func drainOrderHTTP(ctx context.Context, client *http.Client, address string) (orderHTTPDrainReply, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/v1/orders:get", strings.NewReader(`{"order_id":"http-drain"}`))
	if err != nil {
		return orderHTTPDrainReply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return orderHTTPDrainReply{}, err
	}
	defer response.Body.Close()
	reply := orderHTTPDrainReply{code: response.StatusCode}
	err = json.NewDecoder(response.Body).Decode(&reply)
	return reply, err
}

func TestOrderShutdownBudgetCancelsBothProtocols(t *testing.T) {
	registry := openTestRegistry(t)
	name := "orders.drain-timeout." + rand.Text()
	implementation := newWaitingOrder()
	running := runOrderReplica(t, registry, name, implementation, kronos.ShutdownTimeout(time.Second))
	callContext, cancelCalls := context.WithTimeout(t.Context(), 9*time.Second)
	defer cancelCalls()
	var addresses []string
	var results []<-chan orderResult
	for _, protocol := range orderProtocols {
		addresses = append(addresses, waitForInstances(t, registry, name, protocol, 1)[0])
		results = append(results, beginOrderCall(callContext, newDiscoveredOrder(t, registry, name, protocol), string(protocol)))
		awaitOrderSignal(t, implementation.entered)
	}
	started := time.Now()
	running.stop()
	if err := running.wait(t); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded shutdown returned %v", err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Errorf("one-second shutdown budget took %s", elapsed)
	}
	for i, protocol := range orderProtocols {
		if result := awaitOrderSignal(t, results[i]); result.err == nil {
			t.Errorf("%s call finished successfully after force stop", protocol)
		}
		if err := awaitOrderSignal(t, implementation.finished); !errors.Is(err, context.Canceled) {
			t.Errorf("in-flight handler after force stop = %v, want Canceled", err)
		}
		waitForInstances(t, registry, name, protocol, 0)
		assertOrderPortReleased(t, addresses[i])
	}
}

func TestOrderCallerDeadlineAndCancellationReachBothProtocols(t *testing.T) {
	registry := openTestRegistry(t)
	name := "orders.cancel." + rand.Text()
	implementation := newWaitingOrder()
	runOrderReplica(t, registry, name, implementation)
	for _, protocol := range orderProtocols {
		waitForInstances(t, registry, name, protocol, 1)
		orders := newDiscoveredOrder(t, registry, name, protocol)
		for _, scenario := range []struct {
			name    string
			budget  time.Duration
			cancel  bool
			wantErr error
		}{
			{name: "cancel", budget: 5 * time.Second, cancel: true, wantErr: context.Canceled},
			{name: "deadline", budget: time.Second, wantErr: context.DeadlineExceeded},
		} {
			t.Run(string(protocol)+"/"+scenario.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), scenario.budget)
				defer cancel()
				result := beginOrderCall(ctx, orders, "wait-for-"+scenario.name)
				handler := awaitOrderSignal(t, implementation.entered)
				wantDeadline, _ := ctx.Deadline()
				deadline, ok := handler.Deadline()
				if !ok || deadline.Sub(wantDeadline).Abs() > 300*time.Millisecond {
					t.Fatalf("server deadline = %v, want %v", deadline, wantDeadline)
				}
				if scenario.cancel {
					cancel()
				}
				if err := awaitOrderSignal(t, result).err; !errors.Is(err, scenario.wantErr) {
					t.Errorf("client error = %v, want %v", err, scenario.wantErr)
				}
				err := awaitOrderSignal(t, implementation.finished)
				// The handler may see the disconnect before its own timer expires.
				if !errors.Is(err, scenario.wantErr) && !(scenario.wantErr == context.DeadlineExceeded && errors.Is(err, context.Canceled)) {
					t.Errorf("handler error = %v after %s", err, scenario.name)
				}
			})
		}
	}
}

func TestOrderHTTPCancellationReachesBusiness(t *testing.T) {
	registry := openTestRegistry(t)
	name := "orders.http-cancel." + rand.Text()
	implementation := newWaitingOrder()
	address := availableDiagnosticAddress(t)
	runOrderReplica(t, registry, name, implementation, kronos.HTTP(address))
	waitForInstances(t, registry, name, kronos.ProtocolGRPC, 1)
	client := newDiagnosticClient(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := drainOrderHTTP(ctx, client, address)
		result <- err
	}()
	awaitOrderSignal(t, implementation.entered)
	cancel()
	if err := awaitOrderSignal(t, result); !errors.Is(err, context.Canceled) {
		t.Errorf("HTTP client error = %v, want Canceled", err)
	}
	if err := awaitOrderSignal(t, implementation.finished); !errors.Is(err, context.Canceled) {
		t.Errorf("business handler error = %v, want Canceled", err)
	}
}

type withdrawalBarrier struct {
	*etcd.Registry
	started chan struct{}
	proceed chan struct{}
}

func (registry *withdrawalBarrier) Publish(ctx context.Context, name, grpcAddress, microAddress string) (func(context.Context) error, error) {
	withdraw, err := registry.Registry.Publish(ctx, name, grpcAddress, microAddress)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		close(registry.started)
		select {
		case <-registry.proceed:
			return withdraw(ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil
}

type waitingOrder struct {
	calls    atomic.Int32
	entered  chan context.Context
	finished chan error
	release  chan struct{}
}

func newWaitingOrder() *waitingOrder {
	return &waitingOrder{entered: make(chan context.Context, 8), finished: make(chan error, 8), release: make(chan struct{})}
}

func (implementation *waitingOrder) GetOrder(ctx context.Context, request *order.GetOrderRequest) (*order.GetOrderResponse, error) {
	implementation.calls.Add(1)
	implementation.entered <- ctx
	select {
	case <-implementation.release:
		implementation.finished <- nil
		return &order.GetOrderResponse{OrderId: request.GetOrderId(), Status: "after-drain"}, nil
	case <-ctx.Done():
		implementation.finished <- ctx.Err()
		return nil, ctx.Err()
	}
}

type pendingOrder struct {
	orders        *order.OrderServiceClient
	result        <-chan orderResult
	serverContext context.Context
	address       string
}

type orderResult struct {
	response *order.GetOrderResponse
	err      error
}

func newDiscoveredOrder(t *testing.T, registry kronos.RegistryProvider, name string, protocol kronos.WireProtocol, options ...kronos.Option) *order.OrderServiceClient {
	t.Helper()
	options = append([]kronos.Option{kronos.Registry(registry), kronos.Name(name), kronos.Protocol(protocol)}, options...)
	client, err := kronos.NewClient(options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return order.NewOrderServiceClient(client)
}

func beginOrderCall(ctx context.Context, orders *order.OrderServiceClient, id string) <-chan orderResult {
	result := make(chan orderResult, 1)
	go func() {
		response, err := orders.GetOrder(ctx, &order.GetOrderRequest{OrderId: id})
		result <- orderResult{response: response, err: err}
	}()
	return result
}

func awaitOrderSignal[Value any](t *testing.T, signal <-chan Value) Value {
	t.Helper()
	select {
	case value := <-signal:
		return value
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for an Order test event")
		var zero Value
		return zero
	}
}

func assertOrderPortReleased(t *testing.T, address string) {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", address)
	if err != nil {
		t.Fatalf("Order port %s was not released: %v", address, err)
	}
	_ = listener.Close()
}

package consul_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/consul"
)

func TestRegisterLookupAndDeregister(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "PUT /v1/agent/service/register":
			var registration struct {
				ID, Name, Address string
				Port              int
				Tags              []string
				Check             struct {
					GRPC, Status, Interval, Timeout, DeregisterCriticalServiceAfter string
				}
			}
			if err := json.NewDecoder(request.Body).Decode(&registration); err != nil {
				t.Error(err)
			}
			if registration.ID != "worker-1" || registration.Name != "textbackend" || registration.Address != "::1" ||
				registration.Port != 9290 || !slices.Equal(registration.Tags, []string{"grpc"}) || request.URL.Query().Get("replace-existing-checks") != "true" {
				t.Errorf("registration = %+v, request = %s", registration, request.URL)
			}
			check := registration.Check
			if check.GRPC != "[::1]:9290" || check.Status != "critical" || check.Interval != "2s" || check.Timeout != "1s" || check.DeregisterCriticalServiceAfter != "1m" {
				t.Errorf("gRPC check = %+v", check)
			}
		case "GET /v1/health/service/textbackend":
			assertHealthQuery(t, request)
			fmt.Fprint(response, `[
				{"Node":{"Address":"127.0.0.2"},"Service":{"Port":9290}},
				{"Service":{"Address":"::1","Port":9290}},
				{"Service":{"Address":"0.0.0.0","Port":9290}},
				{"Service":{"Address":"::1","Port":9290}},
				{"Service":{"Address":"127.0.0.1","Port":0}}
			]`)
		case "PUT /v1/agent/service/deregister/worker-1":
			http.Error(response, "unknown service", http.StatusNotFound)
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL)
			http.NotFound(response, request)
		}
	}))
	defer service.Close()
	registry, err := consul.New(service.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	instance := consul.Instance{ID: "worker-1", Name: "textbackend", Address: "[::1]:9290"}
	if err := registry.Register(ctx, instance); err != nil {
		t.Fatal(err)
	}
	addresses, err := registry.Lookup(ctx, "textbackend")
	if err != nil || !slices.Equal(addresses, []string{"127.0.0.2:9290", "[::1]:9290"}) {
		t.Fatalf("lookup = %v, %v", addresses, err)
	}
	if err := registry.Deregister(ctx, instance.ID); err != nil {
		t.Fatalf("unknown instance must allow deregistration: %v", err)
	}
	instance.Address = "[::]:9290"
	if err := registry.Register(ctx, instance); err == nil {
		t.Fatal("registered wildcard address")
	}
	if _, err := registry.Lookup(ctx, "../textbackend"); err == nil {
		t.Fatal("lookup accepted multiple path segments")
	}
}

func TestWatchReportsEmptyAndCancelsBlockingQuery(t *testing.T) {
	blocking := make(chan struct{})
	queryCanceled := make(chan struct{})
	service := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assertHealthQuery(t, request)
		switch request.URL.Query().Get("index") {
		case "":
			response.Header().Set("X-Consul-Index", "1")
			fmt.Fprint(response, `[]`)
		case "1":
			response.Header().Set("X-Consul-Index", "2")
			fmt.Fprint(response, `[{"Service":{"ID":"instance-1","Address":"127.0.0.1","Port":9290}}]`)
		case "2":
			response.Header().Set("X-Consul-Index", "3")
			fmt.Fprint(response, `[{"Service":{"ID":"instance-2","Address":"127.0.0.1","Port":9290}}]`)
		case "3":
			response.Header().Set("X-Consul-Index", "4")
			fmt.Fprint(response, `[]`)
		case "4":
			close(blocking)
			<-request.Context().Done()
			close(queryCanceled)
		default:
			t.Errorf("unexpected watch index: %s", request.URL)
			http.Error(response, "unexpected index", http.StatusBadRequest)
		}
	}))
	defer service.Close()
	registry, err := consul.New(service.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	updates := make(chan []string, 5)
	finished := make(chan error, 1)
	go func() {
		finished <- registry.Watch(ctx, "textbackend", func(addresses []string) {
			updates <- slices.Clone(addresses)
			if len(addresses) > 0 {
				addresses[0] = "observer-owned"
			}
		})
	}()
	for _, expected := range [][]string{{}, {"127.0.0.1:9290"}, {}} {
		select {
		case addresses := <-updates:
			if !slices.Equal(addresses, expected) {
				t.Fatalf("snapshot = %v, want %v", addresses, expected)
			}
		case <-ctx.Done():
			t.Fatal("watch did not deliver the next snapshot")
		}
	}
	select {
	case <-blocking:
	case <-ctx.Done():
		t.Fatal("watch did not start a blocking query")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("watch returned %v after cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not stop")
	}
	select {
	case <-queryCanceled:
	case <-time.After(time.Second):
		t.Fatal("blocking HTTP query did not stop")
	}
}

func assertHealthQuery(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Method != http.MethodGet || request.URL.Path != "/v1/health/service/textbackend" ||
		request.URL.Query().Get("tag") != "grpc" || request.URL.Query().Get("passing") != "1" {
		t.Errorf("health query = %s %s", request.Method, request.URL)
	}
}

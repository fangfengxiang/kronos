package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistrationFailureWithdrawsInstanceAndClosesPort(t *testing.T) {
	registered := make(chan string, 1)
	withdrawn := make(chan string, 1)
	agent := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		const registerPath = "/v1/agent/service/register"
		const deregisterPrefix = "/v1/agent/service/deregister/"
		switch {
		case request.Method == http.MethodPut && request.URL.Path == registerPath:
			var instance struct{ ID string }
			if err := json.NewDecoder(request.Body).Decode(&instance); err != nil {
				t.Error(err)
			}
			registered <- instance.ID
			http.Error(response, "registration response unavailable", http.StatusServiceUnavailable)
		case request.Method == http.MethodPut && strings.HasPrefix(request.URL.Path, deregisterPrefix):
			withdrawn <- strings.TrimPrefix(request.URL.Path, deregisterPrefix)
			response.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected Consul request: %s %s", request.Method, request.URL)
			http.NotFound(response, request)
		}
	}))
	defer agent.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	err = run(ctx, []string{"-grpc", address, "-consul", agent.URL})
	if err == nil || !strings.Contains(err.Error(), "register Consul instance") {
		t.Fatalf("text backend registration failure = %v", err)
	}
	var registeredID string
	select {
	case registeredID = <-registered:
	default:
		t.Fatal("registration was never attempted")
	}
	select {
	case withdrawnID := <-withdrawn:
		if registeredID == "" || withdrawnID != registeredID {
			t.Fatalf("withdrawn ID %q does not match registration %q", withdrawnID, registeredID)
		}
	default:
		t.Fatal("possibly registered instance was never withdrawn")
	}
	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("rebind %s: %v", address, err)
	}
	rebound.Close()
}

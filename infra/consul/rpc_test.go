package consul_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fangfengxiang/kronos/infra/consul"
)

func TestRPCPublicationRollsBackItsInstanceOnRegistrationError(t *testing.T) {
	var registeredID, withdrawnID string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/agent/service/register" {
			var registration struct{ ID string }
			if err := json.NewDecoder(request.Body).Decode(&registration); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			registeredID = registration.ID
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		withdrawnID = strings.TrimPrefix(request.URL.Path, "/v1/agent/service/deregister/")
		w.WriteHeader(http.StatusOK)
	}))
	defer agent.Close()
	registry, err := consul.NewRPCRegistry(agent.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	withdraw, err := registry.Publish(t.Context(), "orders", "127.0.0.1:9090", "")
	agent.Close()
	if withdraw != nil || err == nil || registeredID == "" || withdrawnID != registeredID {
		t.Fatalf("failed registration: err=%v registered=%q withdrawn=%q", err, registeredID, withdrawnID)
	}
}

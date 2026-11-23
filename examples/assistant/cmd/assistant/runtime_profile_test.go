package main

import (
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAssistantRejectsWildcardRuntimeProfiles(t *testing.T) {
	addresses := localAddresses(t, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := run(ctx, []string{"-http", addresses[0], "-grpc", addresses[1], "-runtime-profile", "0.0.0.0:0"}, slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "runtime profiles require a loopback TCP listener") {
		t.Fatalf("run with wildcard profile listener: %v", err)
	}
	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatalf("rebind %s after invalid configuration: %v", address, err)
		}
		listener.Close()
	}
}

func assertRuntimeProfile(t *testing.T, ctx context.Context, endpoint string, wantStatus int) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("GET %s = %d, want %d", endpoint, response.StatusCode, wantStatus)
	}
	if wantStatus != http.StatusOK {
		return
	}
	profile, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer profile.Close()
	profileBytes, err := io.Copy(io.Discard, profile)
	if err != nil || profileBytes == 0 {
		t.Fatalf("GET %s = %d profile bytes, error %v", endpoint, profileBytes, err)
	}
}

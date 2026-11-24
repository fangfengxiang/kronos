package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func assertAggregatedMetrics(t *testing.T, ctx context.Context, endpoint string) {
	t.Helper()
	output := scrapeMetrics(t, ctx, endpoint, http.StatusOK)
	for _, sample := range []string{
		`kronos_rpc_requests_total{operation="Run",outcome="success"} 4`,
		`kronos_rpc_requests_total{operation="Run",outcome="error"} 9`,
		`kronos_rpc_requests_total{operation="StreamRun",outcome="success"} 2`,
		`kronos_rpc_requests_total{operation="StreamRun",outcome="error"} 2`,
		`kronos_rpc_request_duration_seconds_count{operation="StreamRun",outcome="success"} 2`,
	} {
		if !strings.Contains(output, sample+"\n") {
			t.Errorf("scrape does not contain %q", sample)
		}
	}
	if strings.Contains(output, "request_id") || strings.Contains(output, "你好") {
		t.Error("scrape contains a request identifier label or private content")
	}
}

func scrapeMetrics(t *testing.T, ctx context.Context, endpoint string, wantStatus int) string {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != wantStatus {
		t.Fatalf("metrics status=%d, want %d, read error=%v", response.StatusCode, wantStatus, err)
	}
	return string(data)
}

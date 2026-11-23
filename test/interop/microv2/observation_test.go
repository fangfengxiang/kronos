//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"slices"
	"testing"

	"github.com/fangfengxiang/kronos/platform/kronos"
)

func captureRPCLogs(t *testing.T) string {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "rpc-log")
	if err != nil {
		t.Fatal(err)
	}
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(original)
		_ = output.Close()
		if t.Failed() {
			log, _ := os.ReadFile(output.Name())
			t.Logf("RPC events:\n%s", log)
		}
	})
	return output.Name()
}

func assertDrainRPCLogs(t *testing.T, path, name string) {
	t.Helper()
	logs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(logs, []byte("arrived-during-withdrawal")) {
		t.Fatal("log contains the Order request value")
	}
	type eventKey struct {
		protocol kronos.WireProtocol
		event    string
		status   string
	}
	counts := make(map[eventKey]int)
	var started, completed, rejected []int64
	lines := bufio.NewScanner(bytes.NewReader(logs))
	for lines.Scan() {
		var record struct {
			Event      string
			Service    string
			Operation  string
			Protocol   kronos.WireProtocol
			Status     string
			Reason     string
			InFlight   *int64 `json:"in_flight"`
			DurationUS *int64 `json:"duration_us"`
			Attempts   int
		}
		if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
			t.Fatalf("decode RPC log: %v", err)
		}
		if record.Service != name || record.Operation != "/kronos.order.v1.OrderService/GetOrder" {
			continue
		}
		counts[eventKey{protocol: record.Protocol, event: record.Event, status: record.Status}]++
		if record.Event != "rpc_started" && (record.DurationUS == nil || *record.DurationUS < 0) {
			t.Fatalf("missing valid duration for %q", record.Event)
		}
		switch record.Event {
		case "rpc_client_finished":
			if record.Attempts != 1 {
				t.Fatalf("client attempts = %d, want one", record.Attempts)
			}
		case "rpc_started", "rpc_finished":
			if record.InFlight == nil {
				t.Fatalf("missing in-flight count for %q", record.Event)
			}
			switch record.Status {
			case "":
				started = append(started, *record.InFlight)
			case "OK":
				completed = append(completed, *record.InFlight)
			case "UNAVAILABLE":
				if record.Reason != "SERVICE_UNAVAILABLE" {
					t.Fatalf("withdrawal RPC reason = %q", record.Reason)
				}
				rejected = append(rejected, *record.InFlight)
			}
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	for _, protocol := range orderProtocols {
		for _, expected := range []eventKey{
			{protocol, "rpc_started", ""},
			{protocol, "rpc_finished", "OK"},
			{protocol, "rpc_finished", "UNAVAILABLE"},
			{protocol, "rpc_client_finished", "OK"},
			{protocol, "rpc_client_finished", "UNAVAILABLE"},
		} {
			if count := counts[expected]; count != 1 {
				t.Errorf("%v logged %d times, want one event", expected, count)
			}
		}
	}
	slices.Sort(completed)
	if !slices.Equal(started, []int64{1, 2}) || !slices.Equal(completed, []int64{0, 1}) || !slices.Equal(rejected, []int64{2, 2}) {
		t.Errorf("in-flight counts = started %v, completed %v, rejected %v", started, completed, rejected)
	}
}

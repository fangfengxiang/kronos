//go:build consul_integration && (linux || darwin)

package consul_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/infra/consul"
)

func TestTextBackendRegistersWithConsul(t *testing.T) {
	agentBinary := os.Getenv("KRONOS_CONSUL_EXECUTABLE")
	if agentBinary == "" {
		t.Fatal("set KRONOS_CONSUL_EXECUTABLE to a Consul binary")
	}
	backendBinary := buildTextBackend(t)
	ports := loopbackPorts(t, 4)
	settings := map[string]any{
		"bind_addr": "127.0.0.1", "client_addr": "127.0.0.1", "disable_update_check": true,
		"ports": map[string]int{"http": ports[0], "server": ports[1], "serf_lan": ports[2], "serf_wan": ports[3],
			"dns": -1, "grpc": -1, "grpc_tls": -1},
	}
	config, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "consul.json")
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	agent := startChild(t, ctx, agentBinary, "agent", "-dev", "-config-file", configPath)
	endpoint := "http://127.0.0.1:" + strconv.Itoa(ports[0])
	registry, err := consul.New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	awaitConsul(t, ctx, registry, agent.done)

	watchContext, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	updates := make(chan []string, 4)
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- registry.Watch(watchContext, "textbackend", func(addresses []string) {
			select {
			case updates <- addresses:
			case <-watchContext.Done():
			}
		})
	}()
	if addresses := nextSnapshot(t, ctx, updates); len(addresses) != 0 {
		t.Fatalf("initial Consul snapshot = %v", addresses)
	}
	backend := startChild(t, ctx, backendBinary, "-grpc", "127.0.0.1:0", "-consul", endpoint)
	addresses := nextSnapshot(t, ctx, updates)
	if len(addresses) != 1 {
		t.Fatalf("healthy Consul snapshot = %v", addresses)
	}
	backendAddress := addresses[0]
	assertConsulHealth(t, ctx, endpoint, addresses)
	client, err := textbackend.NewGRPCClient(ctx, backendAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	response, err := client.Generate(ctx, &textbackend.GenerateRequest{Prompt: "你好"})
	if err != nil || response.GetText() != "backend: 你好" {
		t.Fatalf("healthy instance returned %v, %v", response, err)
	}
	if err := backend.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-backend.done:
		if backend.err != nil {
			t.Fatalf("backend did not stop cleanly: %v", backend.err)
		}
	case <-ctx.Done():
		t.Fatal("backend did not stop after SIGTERM")
	}
	if addresses := nextSnapshot(t, ctx, updates); len(addresses) != 0 {
		t.Fatalf("Consul snapshot after shutdown = %v", addresses)
	}
	assertConsulHealth(t, ctx, endpoint, nil)
	var registered map[string]struct{ Service string }
	getConsulJSON(t, ctx, endpoint+"/v1/agent/services", &registered)
	for _, entry := range registered {
		if entry.Service == "textbackend" {
			t.Fatal("agent still lists the stopped process")
		}
	}
	cancelWatch()
	select {
	case err := <-watchDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("watch returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("watch did not stop")
	}
}

func nextSnapshot(t *testing.T, ctx context.Context, updates <-chan []string) []string {
	t.Helper()
	select {
	case addresses := <-updates:
		return addresses
	case <-ctx.Done():
		t.Fatal("watch did not deliver a snapshot")
		return nil
	}
}

func awaitConsul(t *testing.T, ctx context.Context, registry *consul.Registry, agentDone <-chan struct{}) {
	t.Helper()
	retry := time.NewTicker(30 * time.Millisecond)
	defer retry.Stop()
	for {
		if addresses, err := registry.Lookup(ctx, "textbackend"); err == nil && len(addresses) == 0 {
			return
		}
		select {
		case <-agentDone:
			t.Fatal("Consul agent exited during startup")
		case <-ctx.Done():
			t.Fatal("Consul agent did not become ready")
		case <-retry.C:
		}
	}
}

func assertConsulHealth(t *testing.T, ctx context.Context, endpoint string, expected []string) {
	t.Helper()
	var entries []struct {
		Service struct {
			Address string
			Port    int
		}
	}
	getConsulJSON(t, ctx, endpoint+"/v1/health/service/textbackend?passing=true&tag=grpc", &entries)
	var addresses []string
	for _, entry := range entries {
		addresses = append(addresses, net.JoinHostPort(entry.Service.Address, strconv.Itoa(entry.Service.Port)))
	}
	if !slices.Equal(addresses, expected) {
		t.Fatalf("Consul health API = %v, want %v", addresses, expected)
	}
}

func getConsulJSON(t *testing.T, ctx context.Context, address string, target any) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Consul API = %s", response.Status)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func loopbackPorts(t *testing.T, count int) []int {
	t.Helper()
	ports := make([]int, 0, count)
	for range count {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

func buildTextBackend(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "textbackend")
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "./examples/textbackend/cmd/textbackend")
	command.Dir = "../../.."
	command.Env = append(os.Environ(), "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build text backend: %v\n%s", err, output)
	}
	return binary
}

type childProcess struct {
	command *exec.Cmd
	done    chan struct{}
	err     error
}

func startChild(t *testing.T, ctx context.Context, binary string, arguments ...string) *childProcess {
	t.Helper()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = t.TempDir()
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	child := &childProcess{command: command, done: make(chan struct{})}
	go func() {
		child.err = command.Wait()
		close(child.done)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-child.done
		if t.Failed() {
			t.Logf("%s exited %v\n%s", filepath.Base(binary), child.err, &output)
		}
	})
	return child
}

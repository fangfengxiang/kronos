//go:build e2e

package assistant_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestAssistantCompose(t *testing.T) {
	composeFile, err := filepath.Abs("../../../compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	project := composeProject{name: "kronos-e2e-" + strings.ToLower(rand.Text()), file: composeFile}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if t.Failed() {
			logs, _ := project.run(ctx, "logs", "--no-color", "assistant")
			states, _ := project.run(ctx, "ps", "--all")
			t.Logf("Assistant logs:\n%s\nCompose state:\n%s", logs, states)
		}
		if output, err := project.run(ctx, "down", "--rmi", "local"); err != nil {
			t.Errorf("remove test project: %v\n%s", err, output)
		}
	})
	buildContext, cancelBuild := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancelBuild()
	project.mustRun(t, buildContext, "up", "--build", "--wait", "--wait-timeout", "40")
	cancelBuild()

	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	httpAddress := project.address(t, ctx, "8080")
	grpcAddress := project.address(t, ctx, "9090")
	sseAddress := project.address(t, ctx, "8081")
	legacyAddress := project.address(t, ctx, "9091")
	for _, path := range []string{"/livez", "/readyz"} {
		assertProbe(t, ctx, "http://"+httpAddress+path)
	}
	httpClient, err := assistant.NewHTTPClient(ctx, "http://"+httpAddress, time.Second)
	assertUnary(t, ctx, "HTTP", httpClient, err)
	grpcClient, err := assistant.NewRPCClient(ctx, grpcAddress, time.Second)
	assertUnary(t, ctx, "gRPC", grpcClient, err)
	legacyClient, err := assistant.NewRPCClient(ctx, legacyAddress, time.Second, rpc.WithProtocol(rpc.ProtocolMicroV2))
	assertUnary(t, ctx, "go-micro v2", legacyClient, err)
	assertSSE(t, ctx, sseAddress)

	for _, failure := range []struct {
		name      string
		arguments []string
		logText   string
	}{
		{"missing config", []string{"-config", "/etc/kronos/missing.json"}, "/etc/kronos/missing.json"},
		{"listener conflict", []string{"-config", "/etc/kronos/assistant.json", "-grpc", "0.0.0.0:8080"}, "listen for gRPC"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			command := []string{"run", "--rm", "--no-deps", "-T", "assistant"}
			output, err := project.run(ctx, append(command, failure.arguments...)...)
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(output, []byte(`"event":"app_failed"`)) ||
				!bytes.Contains(output, []byte(failure.logText)) {
				t.Fatalf("expected startup failure with %q: %v\n%s", failure.logText, err, output)
			}
		})
	}

	container := strings.TrimSpace(string(project.mustRun(t, ctx, "ps", "-q", "assistant")))
	project.mustRun(t, ctx, "stop", "assistant")
	state, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .State}}", container).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect stopped container: %v\n%s", err, state)
	}
	var status struct {
		Status   string
		ExitCode int
	}
	if err := json.Unmarshal(state, &status); err != nil || status.Status != "exited" || status.ExitCode != 0 {
		t.Fatalf("stopped container state: %v\n%s", err, state)
	}
	logs := project.mustRun(t, ctx, "logs", "--no-color", "--no-log-prefix", "assistant")
	if !bytes.Contains(logs, []byte(`"event":"app_stopped"`)) {
		t.Fatalf("stopped container is missing the shutdown event:\n%s", logs)
	}
	for _, address := range []string{httpAddress, grpcAddress, sseAddress, legacyAddress} {
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
		if err == nil {
			connection.Close()
			t.Errorf("stopped container still accepts connections on %s", address)
		}
	}
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
}

type composeProject struct {
	name string
	file string
}

func (project composeProject) address(t *testing.T, ctx context.Context, port string) string {
	t.Helper()
	address := strings.TrimSpace(string(project.mustRun(t, ctx, "port", "assistant", port)))
	host, publishedPort, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || publishedPort == "0" || publishedPort == "" {
		t.Fatalf("container port %s mapped to unexpected address %q", port, address)
	}
	return address
}

func (project composeProject) mustRun(t *testing.T, ctx context.Context, arguments ...string) []byte {
	t.Helper()
	output, err := project.run(ctx, arguments...)
	if err != nil {
		t.Fatalf("docker compose %v: %v\n%s", arguments, err, output)
	}
	return output
}

func (project composeProject) run(ctx context.Context, arguments ...string) ([]byte, error) {
	command := []string{"compose", "--project-name", project.name, "--file", project.file}
	return exec.CommandContext(ctx, "docker", append(command, arguments...)...).CombinedOutput()
}

func assertProbe(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	probe, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(probe)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Errorf("probe %s returned %d", url, response.StatusCode)
	}
}

func assertUnary(t *testing.T, ctx context.Context, transport string, client *assistant.Client, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("new %s client: %v", transport, err)
	}
	defer client.Close()
	response, err := client.Run(ctx, &assistant.RunRequest{Prompt: "你好"})
	if err != nil || response.GetText() != "你好" {
		t.Errorf("%s response = (%v, %v)", transport, response, err)
	}
	_, err = client.Run(ctx, &assistant.RunRequest{})
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.InvalidArgument || failure.Reason != assistant.ErrorReasonInvalidPrompt.String() {
		t.Errorf("%s returned the wrong public error: %v", transport, err)
	}
}

func assertSSE(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	streamRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/v1/assistant:streamRun", strings.NewReader(`{"prompt":"你好"}`))
	if err != nil {
		t.Fatal(err)
	}
	streamRequest.Header.Set("Content-Type", "application/json")
	streamRequest.Header.Set("Accept", "text/event-stream")
	response, err := http.DefaultClient.Do(streamRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE returned %d and type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	var text strings.Builder
	deltas, completions := 0, 0
	frames := bufio.NewScanner(response.Body)
	for frames.Scan() {
		data, ok := strings.CutPrefix(frames.Text(), "data: ")
		if !ok {
			continue
		}
		if completions != 0 {
			t.Fatal("SSE delivered data after completion")
		}
		var event struct {
			TextDelta string    `json:"textDelta"`
			Completed *struct{} `json:"completed"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatalf("decode SSE data: %v", err)
		}
		if event.Completed != nil {
			completions++
		}
		if event.TextDelta != "" {
			deltas++
		}
		text.WriteString(event.TextDelta)
	}
	if err := frames.Err(); err != nil || text.String() != "你好" || deltas != 2 || completions != 1 {
		t.Fatalf("SSE text=%q deltas=%d completions=%d error=%v", text.String(), deltas, completions, err)
	}
}

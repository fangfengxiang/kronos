//go:build mysql_integration && (linux || darwin)

package mysql_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/mysql"
)

func TestTextBackendReadsMySQLProfile(t *testing.T) {
	binary := buildTextBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	const firstPrefix = "数据库："
	profile := insertProfile(t, ctx, firstPrefix)
	backend := profile.startBackend(t, ctx, binary, nil)
	const prompt = "你好"
	input := &textbackend.GenerateRequest{Prompt: prompt}
	waitForProfile(t, ctx, backend.done, backend.client, input, firstPrefix+prompt)
	const secondPrefix = "第二版 · "
	profile.update(t, ctx, secondPrefix)
	response, err := backend.client.Generate(ctx, input)
	if err != nil || response.GetText() != secondPrefix+prompt {
		t.Fatalf("updated profile result = %v, %v", response, err)
	}
	if _, err := profile.pool.ExecContext(ctx, "DELETE FROM text_backend_profiles WHERE name = ?", profile.name); err != nil {
		t.Fatal(err)
	}
	_, err = backend.client.Generate(ctx, input)
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != textbackend.ErrorReason_ERROR_REASON_GENERATION_FAILED.String() || failure.Message != "text generation failed" {
		t.Fatalf("missing profile result = %v", err)
	}
	backend.stop(t, ctx)
	missingProfile := exec.CommandContext(ctx, binary, "-grpc", backend.address, "-mysql-profile", profile.name)
	missingProfile.Env = backend.command.Env
	output, err := missingProfile.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(output, []byte("read MySQL text profile")) {
		t.Fatalf("startup with missing profile returned %v\n%s", err, output)
	}
	rebound, err := net.Listen("tcp", backend.address)
	if err != nil {
		t.Fatalf("rebind text backend address: %v", err)
	}
	rebound.Close()
}

type profileFixture struct {
	pool *sql.DB
	dsn  string
	name string
}

func insertProfile(t *testing.T, ctx context.Context, prefix string) profileFixture {
	t.Helper()
	dsn := os.Getenv("KRONOS_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Fatal("set KRONOS_MYSQL_TEST_DSN for an isolated MySQL database")
	}
	pool, err := mysql.Open(ctx, mysql.Config{DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	schema, err := os.ReadFile("../../../examples/textbackend/schema/mysql.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	name := "integration-" + rand.Text()
	if _, err := pool.ExecContext(ctx, "INSERT INTO text_backend_profiles (name, output_prefix) VALUES (?, ?)", name, prefix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := pool.ExecContext(cleanup, "DELETE FROM text_backend_profiles WHERE name = ?", name); err != nil {
			t.Error(err)
		}
	})
	return profileFixture{pool: pool, dsn: dsn, name: name}
}

func (profile profileFixture) update(t *testing.T, ctx context.Context, prefix string) {
	t.Helper()
	if _, err := profile.pool.ExecContext(ctx, "UPDATE text_backend_profiles SET output_prefix = ? WHERE name = ?", prefix, profile.name); err != nil {
		t.Fatal(err)
	}
}

type runningBackend struct {
	client  *textbackend.Client
	command *exec.Cmd
	address string
	done    chan struct{}
	err     error
}

func (profile profileFixture) startBackend(t *testing.T, ctx context.Context, binary string, flags []string, environment ...string) *runningBackend {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	arguments := append([]string{"-grpc", address, "-mysql-profile", profile.name}, flags...)
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Env = append(os.Environ(), "KRONOS_TEXTBACKEND_MYSQL_DSN="+profile.dsn)
	command.Env = append(command.Env, environment...)
	var logs bytes.Buffer
	command.Stdout, command.Stderr = &logs, &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	backend := &runningBackend{command: command, address: address, done: make(chan struct{})}
	go func() {
		backend.err = command.Wait()
		close(backend.done)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-backend.done
		if t.Failed() {
			t.Logf("text backend exited %v\n%s", backend.err, &logs)
		}
	})
	client, err := textbackend.NewGRPCClient(ctx, address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	backend.client = client
	return backend
}

func (backend *runningBackend) stop(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := backend.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-backend.done:
		if backend.err != nil {
			t.Fatalf("text backend did not stop cleanly: %v", backend.err)
		}
	case <-ctx.Done():
		t.Fatal("text backend did not stop after SIGTERM")
	}
}

func waitForProfile(t *testing.T, ctx context.Context, processDone <-chan struct{}, client *textbackend.Client, input *textbackend.GenerateRequest, expected string) {
	t.Helper()
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	for {
		callContext, cancel := context.WithTimeout(ctx, time.Second)
		response, err := client.Generate(callContext, input)
		cancel()
		if err == nil {
			if response.GetText() != expected {
				t.Fatalf("initial profile text = %q, want %q", response.GetText(), expected)
			}
			return
		}
		select {
		case <-processDone:
			t.Fatal("text backend exited before serving its MySQL profile")
		case <-ctx.Done():
			t.Fatalf("text backend did not serve its MySQL profile: %v", err)
		case <-retry.C:
		}
	}
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

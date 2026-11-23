//go:build linux || darwin

package app_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/framework/app"
)

func TestReloadDrainsAcceptedStream(t *testing.T) {
	firstListener := listen(t, "127.0.0.1:0")
	secondListener := listen(t, "127.0.0.1:0")
	firstAddress, secondAddress := firstListener.Addr().String(), secondListener.Addr().String()
	firstListener.Close()
	secondListener.Close()
	directory := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestReloadHelperProcess$")
	command.Env = append(os.Environ(), "KRONOS_RELOAD_TEST_DIRECTORY="+directory,
		"KRONOS_RELOAD_TEST_FIRST="+firstAddress, "KRONOS_RELOAD_TEST_SECOND="+secondAddress)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	initialPID := command.Process.Pid
	result := make(chan error, 1)
	go func() { result <- command.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-initialPID, syscall.SIGKILL) })

	pidFile := filepath.Join(directory, "service.pid")
	if err := waitForFile(ctx, pidFile); err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	endpoint := "http://" + firstAddress
	get := func(path string) *http.Response {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	stream := get("/events")
	defer stream.Body.Close()
	reader := bufio.NewReader(stream.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != fmt.Sprintf("data: %d\n", initialPID) {
		t.Fatalf("initial stream event = %q, %v", line, err)
	}
	if err := command.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	candidateFile := filepath.Join(directory, "candidate.pid")
	if err := waitForFile(ctx, candidateFile); err != nil {
		t.Fatal(err)
	}
	// The first candidate transport has started but the second cannot Accept yet.
	for range 10 {
		response := get("/pid")
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != strconv.Itoa(initialPID) {
			t.Fatalf("request before all listeners started reached %q, %v", body, err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "start"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	candidate, err := os.ReadFile(candidateFile)
	if err != nil {
		t.Fatal(err)
	}
	replacementPID, err := strconv.Atoi(string(candidate))
	if err != nil || replacementPID <= 0 || replacementPID == initialPID {
		t.Fatalf("invalid replacement PID %q", candidate)
	}
	if err := waitForFile(ctx, filepath.Join(directory, "retired")); err != nil {
		t.Fatal(err)
	}
	response := get("/pid")
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != string(candidate) {
		t.Fatalf("request after old listener retired reached %q, %v", body, err)
	}
	select {
	case err := <-result:
		t.Fatalf("old generation exited while its stream was in flight: %v", err)
	default:
	}
	if err := os.WriteFile(filepath.Join(directory, "finish"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if rest, err := io.ReadAll(reader); err != nil || string(rest) != "\ndata: completed\n\n" {
		t.Fatalf("old generation did not complete stream: %q, %v", rest, err)
	}
	if err := receive(t, result); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(replacementPID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
}

// TestReloadHelperProcess is the executable for both generations of the fixture.
func TestReloadHelperProcess(t *testing.T) {
	directory := os.Getenv("KRONOS_RELOAD_TEST_DIRECTORY")
	if directory == "" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	reloads := make(chan os.Signal, 1)
	signal.Notify(reloads, syscall.SIGHUP)
	defer signal.Stop(reloads)
	pidFile := filepath.Join(directory, "service.pid")
	_, previousPIDError := os.Stat(pidFile)
	isReplacement := previousPIDError == nil
	pid := strconv.Itoa(os.Getpid())
	mux := http.NewServeMux()
	mux.HandleFunc("/pid", func(response http.ResponseWriter, _ *http.Request) { fmt.Fprint(response, pid) })
	mux.HandleFunc("/events", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(response, "data: %s\n\n", pid)
		if err := http.NewResponseController(response).Flush(); err != nil {
			return
		}
		if waitForFile(request.Context(), filepath.Join(directory, "finish")) == nil {
			fmt.Fprint(response, "data: completed\n\n")
		}
	})
	binding := func(name, envName string, waitForStart bool) app.Binding {
		return app.Binding{Name: name, Address: os.Getenv(envName), Build: func(listener net.Listener) (app.Server, error) {
			server := &http.Server{Handler: mux}
			return testServer{start: func(ctx context.Context) error {
				if waitForStart && isReplacement {
					marker := filepath.Join(directory, "candidate.tmp")
					if err := os.WriteFile(marker, []byte(pid), 0600); err != nil {
						return err
					}
					if err := os.Rename(marker, filepath.Join(directory, "candidate.pid")); err != nil {
						return err
					}
					if err := waitForFile(ctx, filepath.Join(directory, "start")); err != nil {
						return err
					}
				}
				err := server.Serve(listener)
				if !waitForStart && !isReplacement {
					if markerError := os.WriteFile(filepath.Join(directory, "retired"), nil, 0600); markerError != nil {
						return markerError
					}
				}
				if errors.Is(err, http.ErrServerClosed) {
					return nil
				}
				return err
			}, stop: server.Shutdown}, nil
		}}
	}
	application, err := app.New(app.Config{ShutdownTimeout: 5 * time.Second},
		binding("first", "KRONOS_RELOAD_TEST_FIRST", false), binding("second", "KRONOS_RELOAD_TEST_SECOND", true))
	if err != nil {
		t.Fatal(err)
	}
	err = application.RunWithReload(ctx, app.ReloadConfig{StartupTimeout: 5 * time.Second, PIDFile: pidFile,
		OnError: func(err error) { t.Log(err) }}, reloads)
	if err != nil {
		t.Fatal(err)
	}
}

func waitForFile(ctx context.Context, path string) error {
	retry := time.NewTicker(10 * time.Millisecond)
	defer retry.Stop()
	for {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %s: %w", filepath.Base(path), ctx.Err())
		case <-retry.C:
		}
	}
}

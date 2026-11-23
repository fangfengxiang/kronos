//go:build interop || etcd_interop

package microv2_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const upstreamModule = "github.com/micro/go-micro/v2"

func buildUpstreamFixture(t *testing.T, version string) string {
	t.Helper()
	return buildUpstreamCommand(t, version, ".")
}

func buildUpstreamCommand(t *testing.T, version, packagePath string) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.CopyFS(directory, os.DirFS("testdata")); err != nil {
		t.Fatalf("copy upstream fixture: %v", err)
	}
	// The v2.0.0 etcd registry uses capnslog through its pre-module etcd client.
	manifest := fmt.Sprintf(`module example.com/kronos-microv2-interop

go 1.25.0

require (
 github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f
 %s %s
 golang.org/x/net v0.54.0
)
`, upstreamModule, version)
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	if version == "v2.0.0" {
		output := fixtureGo(t, ctx, directory, "mod", "download", "-json", upstreamModule+"@"+version)
		var module struct{ Dir string }
		if err := json.Unmarshal(output, &module); err != nil || module.Dir == "" {
			t.Fatalf("decode downloaded upstream module directory: %v\n%s", err, output)
		}
		upstreamDirectory := filepath.Join(directory, "upstream")
		if err := os.CopyFS(upstreamDirectory, os.DirFS(module.Dir)); err != nil {
			t.Fatalf("copy upstream source: %v", err)
		}
		// An unused v1 requirement points to a retired code generator repository.
		fixtureGo(t, ctx, directory, "mod", "edit", "-droprequire=github.com/micro/go-micro", filepath.Join(upstreamDirectory, "go.mod"))
		fixtureGo(t, ctx, directory, "mod", "edit", "-replace="+upstreamModule+"=./upstream")
	}
	binary := filepath.Join(directory, "upstream-fixture")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	fixtureGo(t, ctx, directory, "build", "-mod=mod", "-o", binary, packagePath)
	return binary
}

func fixtureGo(t *testing.T, ctx context.Context, directory string, arguments ...string) []byte {
	t.Helper()
	command := exec.CommandContext(ctx, "go", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("go %v: %v\n%s\n%s", arguments, err, output, exit.Stderr)
		}
		t.Fatalf("go %v: %v", arguments, err)
	}
	return output
}

func startUpstreamFixture(t *testing.T, ctx context.Context, binary string) string {
	t.Helper()
	return startFixtureProcess(t, ctx, binary, "serve")
}

func startFixtureProcess(t *testing.T, ctx context.Context, binary string, args ...string) string {
	t.Helper()
	readyFile := filepath.Join(t.TempDir(), "address")
	command := exec.Command(binary, append(args, readyFile)...)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		_ = input.Close()
		t.Fatalf("start official server: %v", err)
	}
	processDone := make(chan struct{})
	var processError error
	go func() {
		processError = command.Wait()
		close(processDone)
	}()
	t.Cleanup(func() {
		_ = input.Close()
		select {
		case <-processDone:
		case <-time.After(5 * time.Second):
			_ = command.Process.Kill()
			<-processDone
			t.Error("official server did not stop after stdin closed")
		}
		if processError != nil || t.Failed() {
			t.Logf("official server exits with %v\n%s", processError, &output)
		}
		if processError != nil {
			t.Errorf("official server exit: %v", processError)
		}
	})

	retry := time.NewTicker(20 * time.Millisecond)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for official server: %v", ctx.Err())
		case <-processDone:
			t.Fatalf("official server exited before publishing an address: %v", processError)
		case <-retry.C:
			address, err := os.ReadFile(readyFile)
			if err == nil {
				return string(address)
			}
			if !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("read official server address: %v", err)
			}
		}
	}
}

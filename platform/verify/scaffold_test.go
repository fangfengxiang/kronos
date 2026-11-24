package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestScaffoldRefusesInvalidAndExistingNames(t *testing.T) {
	repository := t.TempDir()
	for _, name := range []string{"../outside", "main", "type", "2service", "other_name"} {
		if err := scaffoldService(t.Context(), repository, name); err == nil || !strings.Contains(err.Error(), "invalid service name") {
			t.Errorf("scaffold %q = %v", name, err)
		}
	}
	existing := filepath.Join(repository, "examples", "order")
	if err := os.MkdirAll(existing, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(existing, "service.go")
	if err := os.WriteFile(path, []byte("existing source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := scaffoldService(t.Context(), repository, "order"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing service = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "existing source" {
		t.Fatalf("existing source changed: %q, %v", contents, err)
	}
}

func TestScaffoldBuildsFreshService(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("Buf is required to generate a standalone service")
	}
	t.Setenv("GOWORK", "off")
	repository := copyScaffoldWorkspace(t)
	name := "inventory2tier" + strings.ToLower(rand.Text()[:8])
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	if err := scaffoldService(ctx, repository, name); err != nil {
		t.Fatal(err)
	}
	targets, err := protocolTargets(repository)
	if err != nil {
		t.Fatal(err)
	}
	serviceDirectory := filepath.Join("examples", name)
	target := targets[len(targets)-1]
	if len(targets) != len(workspaceProtocolTargets)+1 || !target.standalone || target.serviceDirectory != serviceDirectory {
		t.Fatalf("scaffolded service is missing from protocol targets: %v", targets)
	}
	regenerated := t.TempDir()
	if err := generateProtocolTargets(ctx, repository, regenerated, []protocolTarget{target}); err != nil {
		t.Fatal(err)
	}
	wireDirectory := filepath.Join(serviceDirectory, "internal", "wire")
	if err := compareGeneratedFiles(os.DirFS(filepath.Join(regenerated, wireDirectory)), os.DirFS(filepath.Join(repository, wireDirectory))); err != nil {
		t.Fatal(err)
	}
	contract := filepath.ToSlash(filepath.Join(serviceDirectory, "contract.kronos.pb.go"))
	if err := compareGeneratedFile(os.DirFS(regenerated), os.DirFS(repository), contract); err != nil {
		t.Fatal(err)
	}
	binaries := t.TempDir()
	packageRoot := "./examples/" + name + "/cmd/"
	if err := runCommand(ctx, repository, "go", "build", "-o", binaries, packageRoot+name, packageRoot+name+"-client"); err != nil {
		t.Fatal(err)
	}
	if endpoint := os.Getenv("KRONOS_ETCD_TEST_ADDRESS"); endpoint != "" && runtime.GOOS != "windows" {
		testScaffoldRPC(t, ctx, binaries, name, endpoint)
	}
}

func TestIndependentProjectBuildsFreshService(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("Buf is required to generate an independent service")
	}
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	name := "warehouse" + strings.ToLower(rand.Text()[:8])
	module := "example.com/" + name
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	t.Setenv("GOWORK", "off")
	if err := runCommand(ctx, project, "go", "mod", "init", module); err != nil {
		t.Fatal(err)
	}
	workspace := []string{"work", "init", "."}
	for _, path := range []string{"platform/protogen", "platform/kronos", "framework/app", "framework/rpc", "infra/etcd", "infra/microv2"} {
		workspace = append(workspace, filepath.Join(repository, path))
	}
	if err := runCommand(ctx, project, "go", workspace...); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", filepath.Join(project, "go.work"))
	if err := writeStarter(project, scaffoldNames{
		Package: name, Service: protoServiceName(name), ImportPath: module,
		PluginCommand: []string{"go", "run", kronosModule + "/platform/protogen"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := runCommand(ctx, project, "buf", "lint"); err != nil {
		t.Fatal(err)
	}
	if err := generateTarget(ctx, project, project, "buf.gen.yaml"); err != nil {
		t.Fatal(err)
	}
	binaries := t.TempDir()
	if err := runCommand(ctx, project, "go", "build", "-o", binaries, "./cmd/..."); err != nil {
		t.Fatal(err)
	}
	if endpoint := os.Getenv("KRONOS_ETCD_TEST_ADDRESS"); endpoint != "" && runtime.GOOS != "windows" {
		testScaffoldRPC(t, ctx, binaries, name, endpoint)
	}
}

func copyScaffoldWorkspace(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "..")
	destination := t.TempDir()
	for _, directory := range []string{"core", "framework", "infra", "platform/kronos", "platform/protogen"} {
		if err := os.CopyFS(filepath.Join(destination, directory), os.DirFS(filepath.Join(source, directory))); err != nil {
			t.Fatal(err)
		}
	}
	for _, manifest := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(source, manifest))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(destination, manifest), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(destination, "examples"), 0755); err != nil {
		t.Fatal(err)
	}
	return destination
}

func testScaffoldRPC(t *testing.T, parent context.Context, binaries, name, endpoint string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	server := exec.CommandContext(ctx, filepath.Join(binaries, name))
	server.Env = append(os.Environ(), "KRONOS_ETCD_ENDPOINT="+endpoint,
		"KRONOS_GRPC=127.0.0.1:0", "KRONOS_MICRORPC=127.0.0.1:0")
	var logs bytes.Buffer
	server.Stdout, server.Stderr = &logs, &logs
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var serverError error
	go func() {
		serverError = server.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = server.Process.Kill()
		select {
		case <-done:
			if t.Failed() {
				t.Logf("generated server: %v\n%s", serverError, &logs)
			}
		case <-time.After(5 * time.Second):
			t.Error("generated server did not exit")
		}
	})

	client := filepath.Join(binaries, name+"-client")
	const value = "仓库-收到回执"
	check := time.NewTicker(75 * time.Millisecond)
	defer check.Stop()
	for {
		output, err := exec.CommandContext(ctx, client, "-etcd", endpoint, "-value", value).CombinedOutput()
		if err == nil {
			if strings.TrimSpace(string(output)) != value {
				t.Fatalf("generated gRPC client returned %q", output)
			}
			break
		}
		select {
		case <-done:
			t.Fatalf("generated server failed before discovery: %v", serverError)
		case <-ctx.Done():
			t.Fatalf("generated gRPC client did not discover service: %v\n%s", err, output)
		case <-check.C:
		}
	}
	output, err := exec.CommandContext(ctx, client, "-etcd", endpoint, "-protocol", "micro-v2", "-value", value).CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != value {
		t.Fatalf("generated MicroRPC client: %v\n%s", err, output)
	}
	if err := server.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if serverError != nil {
			t.Fatal(serverError)
		}
	case <-ctx.Done():
		t.Fatal("generated server did not stop")
	}
	t.Log("generated gRPC and MicroRPC clients discovered the service through etcd and received the expected response")
}

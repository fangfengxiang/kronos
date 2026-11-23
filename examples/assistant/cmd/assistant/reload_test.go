//go:build linux || darwin

package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAssistantReloadKeepsAllListeners(t *testing.T) {
	binary := buildAssistant(t)
	addresses := localAddresses(t, 4)
	httpAddress, grpcAddress, sseAddress, microAddress := addresses[0], addresses[1], addresses[2], addresses[3]
	httpEndpoint := "http://" + httpAddress
	stateDirectory := t.TempDir()
	pidFile := filepath.Join(stateDirectory, "assistant.pid")
	configPath := filepath.Join(stateDirectory, "assistant.json")
	startupOptions := map[string]string{
		"http":     httpAddress,
		"grpc":     grpcAddress,
		"sse":      sseAddress,
		"micro-v2": microAddress,
	}
	writeAssistantConfig(t, configPath, startupOptions)
	logFile, err := os.Create(filepath.Join(stateDirectory, "assistant.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-config", configPath, "-reload-pid-file", pidFile, "-reload-timeout", "5s")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	initialPID := command.Process.Pid
	processDone := make(chan struct{})
	var processError error
	go func() {
		processError = command.Wait()
		close(processDone)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-initialPID, syscall.SIGKILL)
		<-processDone
		if t.Failed() {
			logs, _ := os.ReadFile(logFile.Name())
			t.Logf("assistant logs:\n%s", logs)
		}
	})
	waitForReady(t, ctx, httpEndpoint, processDone)
	if pid := readPID(pidFile); pid != initialPID {
		t.Fatalf("initial PID file = %d, want %d", pid, initialPID)
	}

	workingBinary := binary + ".working"
	if err := os.Rename(binary, workingBinary); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./testdata/reloadexit")
	build.Env = append(os.Environ(), "GOWORK=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failing candidate: %v\n%s", err, output)
	}
	if err := command.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitForReloadCondition(t, ctx, "failed candidate reported", func() bool {
		logs, _ := os.ReadFile(logFile.Name())
		return hasLogRecord(logs, func(record processLog) bool {
			return record.Event == "reload_failed" && strings.Contains(record.Error, "exit status 23")
		})
	})
	if pid := readPID(pidFile); pid != initialPID {
		t.Fatalf("failed replacement published PID %d", pid)
	}
	assertAssistantListeners(t, ctx, httpAddress, grpcAddress, sseAddress, microAddress)

	if err := os.Rename(workingBinary, binary); err != nil {
		t.Fatal(err)
	}
	startupOptions["unused-option"] = "invalid"
	writeAssistantConfig(t, configPath, startupOptions)
	if err := command.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitForReloadCondition(t, ctx, "invalid candidate configuration reported", func() bool {
		logs, _ := os.ReadFile(logFile.Name())
		return hasLogRecord(logs, func(record processLog) bool {
			return record.Event == "app_failed" && strings.Contains(record.Error, `unknown option "unused-option"`)
		}) && hasLogRecord(logs, func(record processLog) bool {
			return record.Event == "reload_failed" && strings.Contains(record.Error, "exit status 1")
		})
	})
	if pid := readPID(pidFile); pid != initialPID {
		t.Fatalf("invalid candidate configuration published PID %d", pid)
	}
	assertAssistantListeners(t, ctx, httpAddress, grpcAddress, sseAddress, microAddress)

	delete(startupOptions, "unused-option")
	writeAssistantConfig(t, configPath, startupOptions)
	if err := command.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-processDone:
		if processError != nil {
			t.Fatalf("old generation did not exit cleanly: %v", processError)
		}
	case <-ctx.Done():
		t.Fatal("old generation did not exit after reload")
	}
	replacementPID := readPID(pidFile)
	if replacementPID <= 0 || replacementPID == initialPID {
		t.Fatalf("replacement PID = %d, initial PID = %d", replacementPID, initialPID)
	}
	assertAssistantListeners(t, ctx, httpAddress, grpcAddress, sseAddress, microAddress)

	if err := syscall.Kill(replacementPID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitForReloadCondition(t, ctx, "all listeners released after final SIGTERM", func() bool {
		for _, address := range addresses {
			listener, err := net.Listen("tcp", address)
			if err != nil {
				return false
			}
			listener.Close()
		}
		return true
	})
}

func readPID(path string) int {
	data, _ := os.ReadFile(path)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func waitForReloadCondition(t *testing.T, ctx context.Context, condition string, observe func() bool) {
	t.Helper()
	retry := time.NewTicker(20 * time.Millisecond)
	defer retry.Stop()
	for !observe() {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", condition)
		case <-retry.C:
		}
	}
}

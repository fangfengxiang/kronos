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
	logFile, err := os.Create(filepath.Join(stateDirectory, "assistant.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-http", httpAddress, "-grpc", grpcAddress,
		"-sse", sseAddress, "-micro-v2", microAddress, "-reload-pid-file", pidFile, "-reload-timeout", "5s")
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
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 23\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	waitForReloadCondition(t, ctx, "failed candidate reported", func() bool {
		logs, _ := os.ReadFile(logFile.Name())
		return strings.Contains(string(logs), "exit status 23")
	})
	if pid := readPID(pidFile); pid != initialPID {
		t.Fatalf("failed replacement published PID %d", pid)
	}
	assertAssistantListeners(t, ctx, httpAddress, grpcAddress, sseAddress, microAddress)

	if err := os.Rename(workingBinary, binary); err != nil {
		t.Fatal(err)
	}
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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAssistantConfigPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assistant.json")
	writeAssistantConfig(t, path, map[string]string{
		"http":             "127.0.0.1:18080",
		"grpc":             "127.0.0.1:19090",
		"sse":              "127.0.0.1:18081",
		"shutdown-timeout": "invalid",
	})
	flags := flag.NewFlagSet("assistant", flag.ContinueOnError)
	httpAddress := flags.String("http", "127.0.0.1:8080", "")
	grpcAddress := flags.String("grpc", "127.0.0.1:9090", "")
	sseAddress := flags.String("sse", "", "")
	microAddress := flags.String("micro-v2", "", "")
	shutdownTimeout := flags.Duration("shutdown-timeout", 5*time.Second, "")
	if err := flags.Parse([]string{"-http", "127.0.0.1:8080", "-sse=", "-shutdown-timeout", "10s"}); err != nil {
		t.Fatal(err)
	}
	if err := applyConfigFile(flags, path); err != nil {
		t.Fatal(err)
	}
	if *httpAddress != "127.0.0.1:8080" || *grpcAddress != "127.0.0.1:19090" || *sseAddress != "" || *microAddress != "" {
		t.Fatalf("listeners = HTTP %q, gRPC %q, SSE %q, go-micro v2 %q", *httpAddress, *grpcAddress, *sseAddress, *microAddress)
	}
	if *shutdownTimeout != 10*time.Second {
		t.Fatalf("shutdown timeout = %s", *shutdownTimeout)
	}
}

func TestAssistantRejectsInvalidStartupConfig(t *testing.T) {
	testCases := []struct {
		name    string
		data    string
		message string
	}{
		{name: "unknown option", data: `{"unused":"value"}`, message: `unknown option "unused"`},
		{name: "recursive config", data: `{"config":"other.json"}`, message: `option "config" must be set on the command line`},
		{name: "PID file", data: `{"reload-pid-file":"assistant.pid"}`, message: `option "reload-pid-file" must be set on the command line`},
		{name: "reload timeout", data: `{"reload-timeout":"5s"}`, message: `option "reload-timeout" must be set on the command line`},
		{name: "number", data: `{"sse":8081}`, message: "cannot unmarshal number"},
		{name: "null", data: `{"sse":null}`, message: `option "sse" must be a string`},
		{name: "duration", data: `{"shutdown-timeout":"soon"}`, message: `option "shutdown-timeout"`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "assistant.json")
			if err := os.WriteFile(path, []byte(testCase.data), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			err := run(ctx, []string{"-config", path, "-sse="})
			if err == nil || !strings.Contains(err.Error(), testCase.message) || !strings.Contains(err.Error(), strconv.Quote(path)) {
				t.Fatalf("run error = %v, want %q and the file path", err, testCase.message)
			}
		})
	}
}

func writeAssistantConfig(t *testing.T, path string, options map[string]string) {
	t.Helper()
	data, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

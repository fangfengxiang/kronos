package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowedImport(t *testing.T) {
	const assistant = kronosModule + "/examples/assistant"
	const textBackend = kronosModule + "/examples/textbackend"
	const kratos = "github.com/go-kratos/kratos/v3/transport/http"
	testCases := []struct {
		name       string
		source     string
		dependency string
		allowed    bool
	}{
		{"core within layer", kronosModule + "/core/runtime", kronosModule + "/core/locale", true},
		{"core to framework", kronosModule + "/core/runtime", kronosModule + "/framework/rpc", false},
		{"framework to core", kronosModule + "/framework/rpc", kronosModule + "/core/runtime", true},
		{"framework to infrastructure", kronosModule + "/framework/rpc", kronosModule + "/infra/blades", false},
		{"infrastructure to repository root", kronosModule + "/infra/blades", kronosModule, false},
		{"platform to infrastructure", kronosModule + "/platform/verify", kronosModule + "/infra/blades", true},
		{"app to Kronos", assistant + "/cmd/assistant", kronosModule + "/framework/rpc", true},
		{"app to Kratos", assistant + "/cmd/assistant", kratos, false},
		{"app to gRPC", assistant, "google.golang.org/grpc", false},
		{"binding to Kratos", assistant + "/internal/binding", kratos, true},
		{"generated code to gRPC", assistant + "/internal/wire/v1", "google.golang.org/grpc/status", true},
		{"similarly named business directory", assistant + "/internal/bindings", kratos, false},
		{"text backend command to Kratos", textBackend + "/cmd/textbackend", kratos, false},
		{"text backend contract to gRPC", textBackend, "google.golang.org/grpc", false},
		{"text backend binding to Kratos", textBackend + "/internal/binding", kratos, true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if allowed := allowedImport(testCase.source, testCase.dependency); allowed != testCase.allowed {
				t.Fatalf("allowedImport(%q, %q) = %t, want %t", testCase.source, testCase.dependency, allowed, testCase.allowed)
			}
		})
	}
}

func TestDependenciesRejectsCrossLayerImportInWorkspace(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                  "module " + kronosModule + "\n\ngo 1.25.0\n",
		"go.work":                 "go 1.25.0\n\nuse .\n",
		"infra/source/source.go":  "package source\n",
		"framework/rpc/caller.go": "package rpc\n\nimport _ \"" + kronosModule + "/infra/source\"\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := checkDependencies(t.Context(), root)
	if err == nil || !strings.Contains(err.Error(), "imports forbidden dependency "+kronosModule+"/infra/source") {
		t.Fatalf("dependency check returned %v, want a forbidden import", err)
	}
}

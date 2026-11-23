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
		{"text backend command to Kafka", textBackend + "/cmd/textbackend", "github.com/twmb/franz-go/pkg/kgo", false},
		{"text backend command to MongoDB", textBackend + "/cmd/textbackend", "go.mongodb.org/mongo-driver/v2/mongo", false},
		{"business to etcd SDK", textBackend + "/cmd/textbackend", "go.etcd.io/etcd/client/v3", false},
		{"etcd adapter to lease API", kronosModule + "/infra/etcd", "go.etcd.io/etcd/api/v3/v3rpc/rpctypes", true},
		{"assistant command to telemetry", assistant + "/cmd/assistant", "go.opentelemetry.io/otel/trace", false},
		{"private transport does not own MongoDB", textBackend + "/internal/binding", "go.mongodb.org/mongo-driver/v2/mongo", false},
		{"Kafka adapter subpackage", kronosModule + "/infra/kafka/internal/client", "github.com/twmb/franz-go/pkg/kgo", true},
		{"MongoDB adapter", kronosModule + "/infra/mongodb", "go.mongodb.org/mongo-driver/v2/bson", true},
		{"similar adapter name", kronosModule + "/infra/mongodbextra", "go.mongodb.org/mongo-driver/v2/bson", false},
		{"similar dependency name", textBackend + "/cmd/textbackend", "github.com/twmb/franz-go-helper", true},
		{"trace adapter to Kratos instrumentation", kronosModule + "/infra/oteltrace", "github.com/go-kratos/kratos/contrib/otel/v3/tracing", true},
		{"RPC options to Kratos", kronosModule + "/framework/rpc", kratos, false},
		{"core to gRPC", kronosModule + "/core/runtime", "google.golang.org/grpc/metadata", false},
		{"business to legacy upstream SDK", assistant, "github.com/micro/go-micro/v2/client", false},
		{"business to standard Protobuf", textBackend + "/cmd/textbackend", "google.golang.org/protobuf/proto", true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if allowed := allowedImport(testCase.source, testCase.dependency); allowed != testCase.allowed {
				t.Fatalf("allowedImport(%q, %q) = %t, want %t", testCase.source, testCase.dependency, allowed, testCase.allowed)
			}
		})
	}
}

func TestDependenciesRejectsForbiddenProductionImportsInWorkspace(t *testing.T) {
	root := t.TempDir()
	const kafkaSDK = "github.com/twmb/franz-go"
	const kafkaImport = "import _ \"" + kafkaSDK + "/pkg/kgo\"\n"
	files := map[string]string{
		"go.mod":                  "module " + kronosModule + "\n\ngo 1.25.0\n\nrequire " + kafkaSDK + " v0.0.0\nreplace " + kafkaSDK + " => ./sdk\n",
		"go.work":                 "go 1.25.0\n\nuse .\n",
		"sdk/go.mod":              "module " + kafkaSDK + "\n\ngo 1.25.0\n",
		"sdk/pkg/kgo/kafka.go":    "package kgo\n",
		"infra/source/source.go":  "package source\n",
		"infra/kafka/adapter.go":  "package kafka\n\n" + kafkaImport,
		"framework/rpc/caller.go": "package rpc\n\nimport _ \"" + kronosModule + "/infra/source\"\n",
		"examples/textbackend/cmd/textbackend/main.go":     "package main\n\n" + kafkaImport + "\nfunc main() {}\n",
		"examples/assistant/cmd/assistant/main.go":         "package main\n\nfunc main() {}\n",
		"examples/assistant/cmd/assistant/interop_test.go": "package main\n\n" + kafkaImport,
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
	if err == nil || strings.Count(err.Error(), "imports forbidden dependency") != 2 ||
		!strings.Contains(err.Error(), kronosModule+"/framework/rpc imports forbidden dependency "+kronosModule+"/infra/source") ||
		!strings.Contains(err.Error(), kronosModule+"/examples/textbackend/cmd/textbackend imports forbidden dependency "+kafkaSDK+"/pkg/kgo") {
		t.Fatalf("dependency check returned %v, want the cross-layer import and the production import of Kafka", err)
	}
}

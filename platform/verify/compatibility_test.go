package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBreakingChecksThePublishedFieldContract(t *testing.T) {
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("Buf is required to exercise its compatibility rules")
	}
	repository := t.TempDir()
	const configuration = "version: v2\nmodules:\n  - path: api\nbreaking:\n  use: [FILE]\n"
	schema := filepath.Join(repository, "api", "fixture", "v1", "fixture.proto")
	if err := os.MkdirAll(filepath.Dir(schema), 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repository, "buf.yaml"), configuration)
	const preamble = "syntax = \"proto3\";\npackage fixture.v1;\n"
	write(schema, preamble+"message Result { string value = 1; }\n")
	standalone := filepath.Join(repository, "examples", "fixture")
	standaloneSchema := filepath.Join(standalone, "api", "fixture", "v1", "result.proto")
	if err := os.MkdirAll(filepath.Dir(standaloneSchema), 0755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(standalone, "buf.yaml"), configuration)
	write(standaloneSchema, preamble+"message Result { string value = 1; }\n")

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	for _, arguments := range [][]string{
		{"init", "-q"},
		{"add", "buf.yaml", "api", "examples"},
		{"-c", "user.name=Kronos Test", "-c", "user.email=kronos@example.test", "commit", "-qm", "published contract"},
	} {
		if err := runCommand(ctx, repository, "git", arguments...); err != nil {
			t.Fatal(err)
		}
	}
	write(schema, preamble+"message Result { string value = 1; string note = 2; }\n")
	if err := checkBreaking(ctx, repository, "HEAD"); err != nil {
		t.Fatalf("additive field changes should be compatible: %v", err)
	}
	write(schema, preamble+"message Result { int64 value = 1; string note = 2; }\n")
	if err := checkBreaking(ctx, repository, "HEAD"); err == nil {
		t.Fatal("the gate accepted a wire-incompatible field change")
	}
	write(schema, preamble+"message Result { string value = 1; }\n")
	write(standaloneSchema, preamble+"message Result { int64 value = 1; }\n")
	if err := checkBreaking(ctx, repository, "HEAD"); err == nil {
		t.Fatal("the gate did not check the scaffolded module")
	}
	if err := os.Remove(filepath.Join(standalone, "buf.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := checkBreaking(ctx, repository, "HEAD"); err == nil || !strings.Contains(err.Error(), "published service examples/fixture") {
		t.Fatalf("removed service should be detected, got %v", err)
	}
}

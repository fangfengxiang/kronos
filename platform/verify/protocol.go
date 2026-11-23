package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	bufVersion    = "1.70.0"
	protoTemplate = "tools/proto/buf.gen.yaml"
	generatedWire = "examples/assistant/internal/wire"
)

func checkBufVersion(ctx context.Context, repository string) error {
	command := commandIn(ctx, repository, "buf", "--version")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("Buf %s is required: %w", bufVersion, err)
	}
	if installed := strings.TrimSpace(string(output)); installed != bufVersion {
		return fmt.Errorf("Buf %s is required, found %s", bufVersion, installed)
	}
	return nil
}

func checkProtocol(ctx context.Context, repository string) error {
	if err := checkBufVersion(ctx, repository); err != nil {
		return err
	}
	if err := runCommand(ctx, repository, "buf", "format", "--diff", "--exit-code", "examples/assistant/api"); err != nil {
		return err
	}
	if err := runCommand(ctx, repository, "buf", "lint"); err != nil {
		return err
	}
	outputDirectory, err := os.MkdirTemp("", "kronos-proto-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(outputDirectory)
	if err := runCommand(ctx, repository, "buf", "generate", "--template", protoTemplate, "--output", outputDirectory); err != nil {
		return err
	}
	return compareGeneratedFiles(os.DirFS(filepath.Join(outputDirectory, generatedWire)), os.DirFS(filepath.Join(repository, generatedWire)))
}

func compareGeneratedFiles(regenerated, committed fs.FS) error {
	expected, err := generatedFileNames(regenerated)
	if err != nil {
		return fmt.Errorf("list regenerated files: %w", err)
	}
	actual, err := generatedFileNames(committed)
	if err != nil {
		return fmt.Errorf("list committed generated files: %w", err)
	}
	if !slices.Equal(expected, actual) {
		return fmt.Errorf("generated file list differs: want %v, found %v", expected, actual)
	}
	for _, path := range expected {
		want, err := fs.ReadFile(regenerated, path)
		if err != nil {
			return err
		}
		got, err := fs.ReadFile(committed, path)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("generated file %s does not match Buf output", path)
		}
	}
	return nil
}

func generatedFileNames(root fs.FS) ([]string, error) {
	var paths []string
	err := fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			if !entry.Type().IsRegular() {
				return fmt.Errorf("generated path %s is not a regular file", path)
			}
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

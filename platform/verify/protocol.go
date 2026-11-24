package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const bufVersion = "1.70.0"

type protocolTarget struct {
	serviceDirectory string
	template         string
	standalone       bool
}

var workspaceProtocolTargets = []protocolTarget{
	{serviceDirectory: "examples/assistant", template: "tools/proto/buf.gen.yaml"},
	{serviceDirectory: "examples/textbackend", template: "tools/proto/buf.textbackend.gen.yaml"},
	{serviceDirectory: "examples/order", template: "tools/proto/buf.order.gen.yaml"},
}

func protocolTargets(repository string) ([]protocolTarget, error) {
	targets := slices.Clone(workspaceProtocolTargets)
	examples := filepath.Join(repository, "examples")
	entries, err := os.ReadDir(examples)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validScaffoldName(entry.Name()) {
			continue
		}
		directory := filepath.Join("examples", entry.Name())
		_, err := os.Stat(filepath.Join(repository, directory, "buf.yaml"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(workspaceProtocolTargets, func(target protocolTarget) bool {
			return filepath.FromSlash(target.serviceDirectory) == directory
		}) {
			return nil, fmt.Errorf("%s already uses a repository Buf template", directory)
		}
		targets = append(targets, protocolTarget{serviceDirectory: directory, template: "buf.gen.yaml", standalone: true})
	}
	return targets, nil
}

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
	targets, err := protocolTargets(repository)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := runCommand(ctx, repository, "buf", "format", "--diff", "--exit-code", filepath.Join(target.serviceDirectory, "api")); err != nil {
			return err
		}
		if target.standalone {
			if err := runCommand(ctx, filepath.Join(repository, target.serviceDirectory), "buf", "lint"); err != nil {
				return err
			}
		}
	}
	if err := runCommand(ctx, repository, "buf", "lint"); err != nil {
		return err
	}
	outputDirectory, err := os.MkdirTemp("", "kronos-proto-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(outputDirectory)
	if err := generateProtocolTargets(ctx, repository, outputDirectory, targets); err != nil {
		return err
	}
	for _, target := range targets {
		wireDirectory := filepath.Join(target.serviceDirectory, "internal", "wire")
		if err := compareGeneratedFiles(os.DirFS(filepath.Join(outputDirectory, wireDirectory)), os.DirFS(filepath.Join(repository, wireDirectory))); err != nil {
			return fmt.Errorf("check %s: %w", wireDirectory, err)
		}
		contractPath := filepath.ToSlash(filepath.Join(target.serviceDirectory, "contract.kronos.pb.go"))
		if err := compareGeneratedFile(os.DirFS(outputDirectory), os.DirFS(repository), contractPath); err != nil {
			return err
		}
	}
	return nil
}

func generateProtocols(ctx context.Context, repository, outputDirectory string) error {
	targets, err := protocolTargets(repository)
	if err != nil {
		return err
	}
	return generateProtocolTargets(ctx, repository, outputDirectory, targets)
}

func generateProtocolTargets(ctx context.Context, repository, outputDirectory string, targets []protocolTarget) error {
	for _, target := range targets {
		directory, output := repository, outputDirectory
		if target.standalone {
			directory = filepath.Join(repository, target.serviceDirectory)
			output = filepath.Join(outputDirectory, target.serviceDirectory)
		}
		if err := generateTarget(ctx, directory, output, target.template); err != nil {
			return err
		}
	}
	return nil
}

func generateTarget(ctx context.Context, directory, output, template string) error {
	return runCommand(ctx, directory, "buf", "generate", "--template", template, "--output", output)
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
		if err := compareGeneratedFile(regenerated, committed, path); err != nil {
			return err
		}
	}
	return nil
}

func compareGeneratedFile(regenerated, committed fs.FS, path string) error {
	want, err := fs.ReadFile(regenerated, path)
	if err != nil {
		return fmt.Errorf("read regenerated file: %w", err)
	}
	got, err := fs.ReadFile(committed, path)
	if err != nil {
		return fmt.Errorf("read committed generated file: %w", err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("generated file %s does not match Buf output", path)
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

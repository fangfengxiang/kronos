package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func checkBreaking(ctx context.Context, repository, baseline string) error {
	if err := checkBufVersion(ctx, repository); err != nil {
		return err
	}
	commit, err := commandIn(ctx, repository, "git", "rev-parse", "--verify", "--end-of-options", baseline+"^{commit}").Output()
	if err != nil {
		return fmt.Errorf("resolve protocol baseline %q: %w", baseline, err)
	}
	directory, err := os.MkdirTemp("", "kronos-protocol-baseline-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	archive := filepath.Join(directory, "baseline.tar")
	if err := runCommand(ctx, repository, "git", "archive", "--format=tar", "--output", archive, strings.TrimSpace(string(commit))); err != nil {
		return err
	}
	if err := runCommand(ctx, repository, "buf", "breaking", "--against", archive); err != nil {
		return err
	}
	services, err := standaloneProtocolBaselines(archive)
	if err != nil {
		return err
	}
	for _, service := range services {
		directory := filepath.Join(repository, filepath.FromSlash(service))
		if _, err := os.Stat(filepath.Join(directory, "buf.yaml")); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("published service %s is missing its Buf module", service)
			}
			return err
		}
		if err := runCommand(ctx, directory, "buf", "breaking", "--against", archive+"#subdir="+service); err != nil {
			return err
		}
	}
	return nil
}

func standaloneProtocolBaselines(archive string) ([]string, error) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := tar.NewReader(file)
	var services []string
	for {
		entry, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return services, nil
		}
		if err != nil {
			return nil, err
		}
		parts := strings.Split(entry.Name, "/")
		if entry.Typeflag == tar.TypeReg && len(parts) == 3 && parts[0] == "examples" &&
			validScaffoldName(parts[1]) && parts[2] == "buf.yaml" {
			services = append(services, parts[0]+"/"+parts[1])
		}
	}
}

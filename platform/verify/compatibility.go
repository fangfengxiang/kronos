package main

import (
	"context"
	"fmt"
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
	return runCommand(ctx, repository, "buf", "breaking", "--against", archive)
}

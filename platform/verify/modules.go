package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func testModules(ctx context.Context, repository string) error {
	directories, err := workspaceModules(ctx, repository)
	if err != nil {
		return err
	}
	for _, directory := range directories {
		test := isolatedGo(ctx, directory, "test", "-race", "-count=1", "./...")
		test.Stdout = os.Stdout
		if err := test.Run(); err != nil {
			return fmt.Errorf("test module %s: %w", directory, err)
		}
	}
	return nil
}

func workspaceModules(ctx context.Context, repository string) ([]string, error) {
	workspacePath := filepath.Join(repository, "go.work")
	command := commandIn(ctx, repository, "go", "work", "edit", "-json", workspacePath)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read workspace %s: %w", workspacePath, err)
	}
	var workspace struct {
		Use []struct{ DiskPath string }
	}
	if err := json.Unmarshal(output, &workspace); err != nil {
		return nil, fmt.Errorf("decode workspace %s: %w", workspacePath, err)
	}
	if len(workspace.Use) == 0 {
		return nil, errors.New("workspace does not list any modules")
	}
	directories := make([]string, 0, len(workspace.Use))
	for _, entry := range workspace.Use {
		directory := entry.DiskPath
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(repository, directory)
		}
		directories = append(directories, directory)
	}
	return directories, nil
}

func isolatedGo(ctx context.Context, directory string, arguments ...string) *exec.Cmd {
	command := commandIn(ctx, directory, "go", arguments...)
	command.Env = append(os.Environ(), "GOWORK=off")
	return command
}

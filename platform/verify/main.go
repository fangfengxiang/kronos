// Command verify runs the Kronos repository checks from its root directory.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("usage: go run ./platform/verify {test|deps|proto|generate}")
	}
	repository, err := os.Getwd()
	if err != nil {
		return err
	}
	switch arguments[0] {
	case "test":
		return testModules(ctx, repository)
	case "deps":
		return checkDependencies(ctx, repository)
	case "proto":
		return checkProtocol(ctx, repository)
	case "generate":
		if err := checkBufVersion(ctx, repository); err != nil {
			return err
		}
		return runCommand(ctx, repository, "buf", "generate", "--template", protoTemplate)
	default:
		return fmt.Errorf("unknown verification command %q", arguments[0])
	}
}

func commandIn(ctx context.Context, directory, name string, arguments ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Stderr = os.Stderr
	return command
}

func runCommand(ctx context.Context, directory, name string, arguments ...string) error {
	command := commandIn(ctx, directory, name, arguments...)
	command.Stdout = os.Stdout
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, arguments, err)
	}
	return nil
}

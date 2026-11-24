// Command verify runs Kronos repository checks and scaffolds services.
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
	const usage = "usage: verify {test|deps|proto|generate|breaking <baseline>|scaffold <name>|scaffold-project <module> <revision>}"
	if len(arguments) == 0 {
		return errors.New(usage)
	}
	repository, err := os.Getwd()
	if err != nil {
		return err
	}
	if len(arguments) == 3 && arguments[0] == "scaffold-project" {
		return scaffoldProject(ctx, repository, arguments[1], arguments[2])
	}
	if len(arguments) == 2 {
		switch arguments[0] {
		case "breaking":
			return checkBreaking(ctx, repository, arguments[1])
		case "scaffold":
			return scaffoldService(ctx, repository, arguments[1])
		}
	}
	if len(arguments) != 1 || arguments[0] == "breaking" || arguments[0] == "scaffold" || arguments[0] == "scaffold-project" {
		return errors.New(usage)
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
		return generateProtocols(ctx, repository, repository)
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

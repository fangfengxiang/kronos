package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed scaffold/*
var scaffoldTemplates embed.FS

var serviceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)
var scaffoldRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func validScaffoldName(name string) bool {
	return serviceNamePattern.MatchString(name) && token.IsIdentifier(name) && name != "main"
}

type scaffoldNames struct {
	Package       string
	Service       string
	ImportPath    string
	PluginCommand []string
	Standalone    bool
}

func protoServiceName(packageName string) string {
	var name strings.Builder
	wordStart := true
	for _, character := range packageName {
		if wordStart && character >= 'a' && character <= 'z' {
			character -= 'a' - 'A'
		}
		name.WriteRune(character)
		// The Go Protobuf generator treats a number as a word boundary.
		wordStart = character >= '0' && character <= '9'
	}
	return name.String() + "Service"
}

func scaffoldService(ctx context.Context, repository, name string) error {
	if !validScaffoldName(name) {
		return fmt.Errorf("invalid service name %q: use a lower-case Go package name, starting with a letter", name)
	}
	examples := filepath.Join(repository, "examples")
	destination := filepath.Join(examples, name)
	if err := checkScaffoldDestination(destination); err != nil {
		return err
	}
	if err := checkBufVersion(ctx, repository); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(examples, ".kronos-scaffold-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := writeStarter(staging, scaffoldNames{
		Package: name, Service: protoServiceName(name),
		ImportPath:    kronosModule + "/examples/" + name,
		PluginCommand: []string{"go", "-C", "../../platform/protogen", "run", "."},
	}); err != nil {
		return err
	}
	if err := runCommand(ctx, staging, "buf", "lint"); err != nil {
		return err
	}
	if err := generateTarget(ctx, staging, staging, "buf.gen.yaml"); err != nil {
		return err
	}
	if err := installScaffold(staging, destination); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "created examples/%s\n", name)
	return nil
}

func scaffoldProject(ctx context.Context, parent, module, revision string) error {
	name := path.Base(module)
	if !validScaffoldName(name) || !strings.Contains(module, "/") || path.Clean(module) != module {
		return fmt.Errorf("invalid project module %q: use a Go module path ending in a lower-case service name", module)
	}
	if !scaffoldRevisionPattern.MatchString(revision) {
		return fmt.Errorf("invalid Kronos revision %q: use a full commit hash", revision)
	}
	destination := filepath.Join(parent, name)
	if err := checkScaffoldDestination(destination); err != nil {
		return err
	}
	if err := checkBufVersion(ctx, parent); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".kronos-scaffold-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	if err := isolatedGo(ctx, staging, "mod", "init", module).Run(); err != nil {
		return fmt.Errorf("initialize project module: %w", err)
	}
	if err := isolatedGo(ctx, staging, "get",
		kronosModule+"/platform/kronos@"+revision, kronosModule+"/infra/etcd@"+revision).Run(); err != nil {
		return fmt.Errorf("install Kronos dependencies: %w", err)
	}
	if err := writeStarter(staging, scaffoldNames{
		Package: name, Service: protoServiceName(name), ImportPath: module, Standalone: true,
		PluginCommand: []string{"go", "run", kronosModule + "/platform/protogen@" + revision},
	}); err != nil {
		return err
	}
	if err := runCommand(ctx, staging, "buf", "lint"); err != nil {
		return err
	}
	if err := generateTarget(ctx, staging, staging, "buf.gen.yaml"); err != nil {
		return err
	}
	if err := isolatedGo(ctx, staging, "mod", "tidy").Run(); err != nil {
		return fmt.Errorf("resolve project dependencies: %w", err)
	}
	if err := installScaffold(staging, destination); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "created", name)
	return nil
}

func checkScaffoldDestination(destination string) error {
	_, err := os.Lstat(destination)
	switch {
	case err == nil:
		return fmt.Errorf("service directory already exists: %s", destination)
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return err
	}
}

func installScaffold(staging, destination string) (installError error) {
	if err := os.Mkdir(destination, 0755); err != nil {
		return fmt.Errorf("create service directory: %w", err)
	}
	defer func() {
		if installError != nil {
			installError = errors.Join(installError, os.RemoveAll(destination))
		}
	}()
	return os.CopyFS(destination, os.DirFS(staging))
}

func writeStarter(directory string, names scaffoldNames) error {
	templates, err := template.New("scaffold").Option("missingkey=error").ParseFS(scaffoldTemplates, "scaffold/*")
	if err != nil {
		return err
	}
	for _, fixture := range []struct{ template, output string }{
		{template: "service.proto.tmpl", output: filepath.Join("api", "kronos", names.Package, "v1", names.Package+".proto")},
		{template: "buf.yaml.tmpl", output: "buf.yaml"},
		{template: "buf.gen.yaml.tmpl", output: "buf.gen.yaml"},
		{template: "service.go.tmpl", output: "service.go"},
		{template: "server.go.tmpl", output: filepath.Join("cmd", names.Package, "main.go")},
		{template: "client.go.tmpl", output: filepath.Join("cmd", names.Package+"-client", "main.go")},
		{template: "README.md.tmpl", output: "README.md"},
	} {
		rendered := new(bytes.Buffer)
		if err := templates.ExecuteTemplate(rendered, fixture.template, names); err != nil {
			return err
		}
		content := rendered.Bytes()
		if strings.HasSuffix(fixture.output, ".go") {
			content, err = format.Source(content)
			if err != nil {
				return fmt.Errorf("format %s: %w", fixture.output, err)
			}
		}
		path := filepath.Join(directory, fixture.output)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, content, 0644); err != nil {
			return err
		}
	}
	return nil
}

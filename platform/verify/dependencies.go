package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const kronosModule = "github.com/fangfengxiang/kronos"

func checkDependencies(ctx context.Context, repository string) error {
	directories, err := workspaceModules(ctx, repository)
	if err != nil {
		return err
	}
	var violations []error
	for _, directory := range directories {
		command := isolatedGo(ctx, directory, "list", "-json=ImportPath,Imports", "./...")
		output, err := command.Output()
		if err != nil {
			return fmt.Errorf("list dependencies for %s: %w", directory, err)
		}
		decoder := json.NewDecoder(bytes.NewReader(output))
		for {
			var pkg struct {
				ImportPath string
				Imports    []string
			}
			if err := decoder.Decode(&pkg); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return fmt.Errorf("decode dependencies for %s: %w", directory, err)
			}
			for _, dependency := range pkg.Imports {
				if !allowedImport(pkg.ImportPath, dependency) {
					violations = append(violations, fmt.Errorf("%s imports forbidden dependency %s", pkg.ImportPath, dependency))
				}
			}
		}
	}
	return errors.Join(violations...)
}

func allowedImport(source, dependency string) bool {
	const examples = kronosModule + "/examples/"
	if relative, isExample := strings.CutPrefix(source, examples); isExample &&
		(hasPackagePrefix(dependency, "github.com/go-kratos/kratos") || hasPackagePrefix(dependency, "google.golang.org/grpc")) {
		service, _, _ := strings.Cut(relative, "/")
		return hasPackagePrefix(source, examples+service+"/internal/binding") || hasPackagePrefix(source, examples+service+"/internal/wire")
	}
	if !hasPackagePrefix(dependency, kronosModule) {
		return true
	}
	sourceLayer := layerOf(source)
	targetLayer := layerOf(dependency)
	switch sourceLayer {
	case "core":
		return targetLayer == "core"
	case "infra", "framework":
		return targetLayer == sourceLayer || targetLayer == "core"
	case "platform":
		return targetLayer == "platform" || targetLayer == "core" || targetLayer == "infra" || targetLayer == "framework"
	default:
		return true
	}
}

func layerOf(packagePath string) string {
	relative, _ := strings.CutPrefix(packagePath, kronosModule+"/")
	layer, _, _ := strings.Cut(relative, "/")
	return layer
}

func hasPackagePrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

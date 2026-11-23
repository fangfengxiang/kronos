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

const (
	kratosImportPrefix = "github.com/go-kratos/kratos"
	grpcImportPrefix   = "google.golang.org/grpc"
)

// Owners are repository-relative import paths. A nil list reserves an SDK for
// private transport bindings or test fixtures.
var sdkOwnersByImportPrefix = map[string][]string{
	kratosImportPrefix: {"framework/app", "framework/log", "infra/oteltrace", "infra/runtimeprofile", "platform/kronos/internal/transport"},
	grpcImportPrefix:   {"platform/kronos/internal/transport"},
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2": {"infra/aliyunoss"},
	"github.com/cloudflare/tableflip":              {"framework/app"},
	"github.com/coreos/etcd":                       nil,
	"github.com/go-kratos/blades":                  {"infra/blades"},
	"github.com/go-sql-driver/mysql":               {"infra/mysql"},
	"github.com/hashicorp/consul/api":              {"infra/consul"},
	"github.com/micro/go-micro":                    nil,
	"github.com/prometheus/client_golang":          {"infra/prometheus"},
	"github.com/prometheus/client_model":           {"infra/prometheus"},
	"github.com/redis/go-redis":                    {"infra/redis"},
	"github.com/twmb/franz-go":                     {"infra/kafka"},
	"go.etcd.io/etcd":                              {"infra/etcd"},
	"go.mongodb.org/mongo-driver":                  {"infra/mongodb"},
	"go.opentelemetry.io/otel":                     {"infra/oteltrace"},
	"go.opentelemetry.io/proto/otlp":               {"infra/oteltrace"},
}

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
	if !hasPackagePrefix(dependency, kronosModule) {
		return allowedExternalImport(source, dependency)
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

func allowedExternalImport(source, dependency string) bool {
	const examples = kronosModule + "/examples/"
	if relative, isExample := strings.CutPrefix(source, examples); isExample &&
		(hasPackagePrefix(dependency, kratosImportPrefix) || hasPackagePrefix(dependency, grpcImportPrefix)) {
		service, _, _ := strings.Cut(relative, "/")
		serviceRoot := examples + service
		return hasPackagePrefix(source, serviceRoot+"/internal/binding") || hasPackagePrefix(source, serviceRoot+"/internal/wire")
	}
	for importPrefix, owners := range sdkOwnersByImportPrefix {
		if !hasPackagePrefix(dependency, importPrefix) {
			continue
		}
		for _, owner := range owners {
			if hasPackagePrefix(source, kronosModule+"/"+owner) {
				return true
			}
		}
		return false
	}
	return true
}

func layerOf(packagePath string) string {
	relative, _ := strings.CutPrefix(packagePath, kronosModule+"/")
	layer, _, _ := strings.Cut(relative, "/")
	return layer
}

func hasPackagePrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

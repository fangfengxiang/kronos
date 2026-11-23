//go:build etcd_interop

package microv2_test

import (
	"context"
	"crypto/rand"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/etcd"
)

func TestGoMicroV2RegistryReadsPublishedMicroAddress(t *testing.T) {
	endpoint := os.Getenv("KRONOS_ETCD_TEST_ADDRESS")
	if endpoint == "" {
		t.Fatal("set KRONOS_ETCD_TEST_ADDRESS to an isolated etcd host:port")
	}
	registry, err := etcd.New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	const grpcAddress = "127.0.0.1:9090"
	const microAddress = "127.0.0.1:9091"
	service := "go.micro.srv.order." + rand.Text()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	withdraw, err := registry.Publish(ctx, service, grpcAddress, microAddress)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := withdraw(ctx); err != nil {
			t.Error(err)
		}
	})

	for _, version := range []string{"v2.0.0", "v2.9.1"} {
		t.Run(version, func(t *testing.T) {
			binary := buildUpstreamCommand(t, version, "./etcdregistry")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			output, err := exec.CommandContext(ctx, binary, endpoint, service).CombinedOutput()
			if err != nil {
				t.Fatalf("official Registry GetService: %v\n%s", err, output)
			}
			fields := strings.Fields(string(output))
			if len(fields) != 4 || fields[0] != service || fields[1] != "1.0.0" || fields[3] != microAddress {
				t.Fatalf("official Registry nodes = %q, want only %s", output, microAddress)
			}
		})
	}
}

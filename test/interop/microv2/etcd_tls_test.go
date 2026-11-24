//go:build etcd_interop && (linux || darwin)

package microv2_test

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/etcd"
)

func TestOrderWithMutualTLSEtcd(t *testing.T) {
	endpoint := os.Getenv("KRONOS_ETCD_TEST_TLS_ENDPOINT")
	if endpoint == "" {
		t.Skip("set KRONOS_ETCD_TEST_TLS_ENDPOINT and the Order certificate file settings")
	}
	rootPEM, err := os.ReadFile(os.Getenv("KRONOS_ETCD_CA_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		t.Fatal("etcd test CA contains no certificate")
	}
	certificate, err := tls.LoadX509KeyPair(os.Getenv("KRONOS_ETCD_CERT_FILE"), os.Getenv("KRONOS_ETCD_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	valid := &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{certificate}}
	registry := testEtcdRegistry(t, endpoint, valid)
	name := "orders.secure." + rand.Text()
	lookupContext, cancelLookup := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancelLookup()
	if _, err := registry.Lookup(lookupContext, name, "grpc"); err != nil {
		t.Fatalf("mutually authenticated lookup failed: %v", err)
	}
	for _, scenario := range []struct {
		name string
		tls  *tls.Config
	}{
		{"unknown authority", &tls.Config{RootCAs: x509.NewCertPool(), Certificates: valid.Certificates}},
		{"missing client certificate", &tls.Config{RootCAs: roots}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			untrusted := testEtcdRegistry(t, endpoint, scenario.tls)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if _, err := untrusted.Lookup(ctx, name, "grpc"); err == nil {
				t.Fatal("etcd accepted an unauthenticated registry call")
			}
		})
	}
	process := startOrderProcess(t, endpoint, name)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	waitForOrderAddresses(t, ctx, registry, name, process)
	binary := buildOrderCommand(t, "order-client")
	assertOrderClientCommand(t, binary, endpoint, name, "confirmed")
	assertOrderClientCommand(t, binary, endpoint, name, "confirmed", "-protocol", "micro-v2")
	process.stop(t, 12*time.Second)
	for _, protocol := range orderProtocols {
		if addresses, err := registry.Lookup(t.Context(), name, string(protocol)); err != nil || len(addresses) != 0 {
			t.Errorf("after shutdown %s addresses = %v, %v", protocol, addresses, err)
		}
	}
}

func testEtcdRegistry(t *testing.T, endpoint string, security *tls.Config) *etcd.Registry {
	t.Helper()
	registry, err := etcd.NewWithConfig(etcd.Config{Endpoints: []string{endpoint}, TLS: security})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	})
	return registry
}

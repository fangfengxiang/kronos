//go:build etcd_integration

package etcd

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestProtocolAddressesShareOneLease(t *testing.T) {
	registry := integrationRegistry(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	service := "go.micro.srv.order." + rand.Text()
	first := publishInstance(t, ctx, registry, service, "127.0.0.1:9090", "127.0.0.1:9091")
	grpcKey, grpcLease, grpcRevision := publishedKey(t, ctx, registry, grpcPrefix+service+"/")
	microKey, microLease, microRevision := publishedKey(t, ctx, registry, microPrefix+service+"/")
	if grpcLease == 0 || grpcLease != microLease || grpcRevision != microRevision ||
		grpcKey != grpcPrefix+service+"/"+microKey[len(microPrefix+service+"/"):] {
		t.Fatalf("protocol records differ in instance ID, lease, or creation revision")
	}
	publishInstance(t, ctx, registry, service, "127.0.0.1:9092", "")
	assertAddresses(t, ctx, registry, service, "grpc", "127.0.0.1:9090", "127.0.0.1:9092")
	assertAddresses(t, ctx, registry, service, "micro-v2", "127.0.0.1:9091")
	for range 2 {
		if err := first(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertAddresses(t, ctx, registry, service, "grpc", "127.0.0.1:9092")
	assertAddresses(t, ctx, registry, service, "micro-v2")

	canceled, stop := context.WithCancel(ctx)
	stop()
	if withdraw, err := registry.Publish(canceled, service, "", "127.0.0.1:9093"); withdraw != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publication = %v", err)
	}
	assertAddresses(t, ctx, registry, service, "micro-v2")
}

func TestRenewalAndReplacementOfLostLease(t *testing.T) {
	registry := integrationRegistry(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	service := "orders.renewal." + rand.Text()
	publishContext, cancelPublish := context.WithCancel(ctx)
	publishInstance(t, publishContext, registry, service, "", "127.0.0.1:9091")
	cancelPublish()
	recordPrefix := microPrefix + service + "/"
	key, lease, _ := publishedKey(t, ctx, registry, recordPrefix)

	afterTTL := time.NewTimer(time.Duration(registry.leaseTTLSeconds)*time.Second + 500*time.Millisecond)
	defer afterTTL.Stop()
	select {
	case <-afterTTL.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, renewedLease, _ := publishedKey(t, ctx, registry, recordPrefix); renewedLease != lease {
		t.Fatal("the original lease did not survive its TTL")
	}
	if _, err := registry.client.Revoke(ctx, lease); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, func() bool {
		result, err := registry.client.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		return len(result.Kvs) == 1 && result.Kvs[0].Lease > 0 && clientv3.LeaseID(result.Kvs[0].Lease) != lease
	})
	assertAddresses(t, ctx, registry, service, "micro-v2", "127.0.0.1:9091")
}

func TestLeaseExpiresWhenRenewalStops(t *testing.T) {
	publisher := integrationRegistry(t)
	observer := integrationRegistry(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	service := "orders.expiry." + rand.Text()
	_, err := publisher.Publish(ctx, service, "127.0.0.1:9090", "127.0.0.1:9091")
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, func() bool {
		legacyAddresses, err := observer.Lookup(ctx, service, "micro-v2")
		if err != nil {
			t.Fatal(err)
		}
		return len(legacyAddresses) == 0
	})
	assertAddresses(t, ctx, observer, service, "grpc")
}

func integrationRegistry(t *testing.T) *Registry {
	t.Helper()
	endpoint := os.Getenv("KRONOS_ETCD_TEST_ADDRESS")
	if endpoint == "" {
		t.Fatal("set KRONOS_ETCD_TEST_ADDRESS to an isolated etcd")
	}
	registry, err := New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	registry.leaseTTLSeconds = 3
	registry.renewInterval = 500 * time.Millisecond
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Error(err)
		}
	})
	return registry
}

func publishInstance(t *testing.T, ctx context.Context, registry *Registry, service, grpcAddress, microAddress string) func(context.Context) error {
	t.Helper()
	withdraw, err := registry.Publish(ctx, service, grpcAddress, microAddress)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := withdraw(ctx); err != nil {
			t.Error(err)
		}
	})
	return withdraw
}

func publishedKey(t *testing.T, ctx context.Context, registry *Registry, prefix string) (string, clientv3.LeaseID, int64) {
	t.Helper()
	result, err := registry.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Kvs) != 1 {
		t.Fatalf("etcd prefix %q has %d keys, want 1", prefix, len(result.Kvs))
	}
	entry := result.Kvs[0]
	return string(entry.Key), clientv3.LeaseID(entry.Lease), entry.CreateRevision
}

func assertAddresses(t *testing.T, ctx context.Context, registry *Registry, service, protocol string, expected ...string) {
	t.Helper()
	addresses, err := registry.Lookup(ctx, service, protocol)
	if err != nil || !slices.Equal(addresses, expected) {
		t.Fatalf("Lookup %s = %v, %v, want %v", protocol, addresses, err, expected)
	}
}

func waitFor(t *testing.T, ctx context.Context, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for !condition() {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

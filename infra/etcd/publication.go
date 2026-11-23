package etcd

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type instanceRecord struct {
	key   string
	value string
}

// Publish creates one instance with any supplied protocol addresses. The
// returned function stops renewal and revokes the lease. The startup context
// bounds the first write. The registry owns the renewal until withdrawal.
func (registry *Registry) Publish(ctx context.Context, serviceName, grpcAddress, microRPCAddress string) (func(context.Context) error, error) {
	if !serviceNamePattern.MatchString(serviceName) ||
		(grpcAddress == "" && microRPCAddress == "") ||
		(grpcAddress != "" && !validAddress(grpcAddress)) ||
		(microRPCAddress != "" && !validAddress(microRPCAddress)) {
		return nil, fmt.Errorf("%w: require a service name and at least one concrete protocol address", ErrInvalidConfig)
	}
	instanceID := rand.Text()
	records := instanceRecords(serviceName, instanceID, grpcAddress, microRPCAddress)
	leaseID, err := registry.publishRecords(ctx, records)
	if err != nil {
		return nil, err
	}
	renewContext, cancelRenewal := context.WithCancel(registry.client.Ctx())
	instance := &publication{
		registry: registry, serviceName: serviceName, instanceID: instanceID, records: records, leaseID: leaseID,
		stopRenewal: cancelRenewal, done: make(chan struct{}),
	}
	slog.InfoContext(ctx, "etcd service published", "event", "registry_published", "service", serviceName, "instance_id", instanceID)
	go instance.renew(renewContext)
	return instance.withdraw, nil
}

func instanceRecords(serviceName, instanceID, grpcAddress, microRPCAddress string) []instanceRecord {
	records := make([]instanceRecord, 0, 2)
	if grpcAddress != "" {
		records = append(records, instanceRecord{key: grpcPrefix + serviceName + "/" + instanceID, value: grpcAddress})
	}
	if microRPCAddress != "" {
		document, _ := json.Marshal(microService{
			Name: serviceName, Version: "1.0.0", Nodes: []microNode{{ID: instanceID, Address: microRPCAddress}},
		})
		records = append(records, instanceRecord{key: microPrefix + serviceName + "/" + instanceID, value: string(document)})
	}
	return records
}

func (registry *Registry) publishRecords(ctx context.Context, records []instanceRecord) (clientv3.LeaseID, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, registry.requestTimeout)
	defer cancel()
	lease, err := registry.client.Grant(ctx, registry.leaseTTLSeconds)
	if err != nil {
		return 0, operationError(ctx, "grant service lease", err)
	}
	puts := make([]clientv3.Op, 0, len(records))
	for _, record := range records {
		puts = append(puts, clientv3.OpPut(record.key, record.value, clientv3.WithLease(lease.ID)))
	}
	_, err = registry.client.Txn(ctx).Then(puts...).Commit()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return 0, errors.Join(operationError(ctx, "publish service addresses", err), registry.revoke(context.WithoutCancel(ctx), lease.ID))
	}
	return lease.ID, nil
}

func (registry *Registry) revoke(ctx context.Context, leaseID clientv3.LeaseID) error {
	if leaseID == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, registry.requestTimeout)
	defer cancel()
	_, err := registry.client.Revoke(ctx, leaseID)
	if err != nil && !errors.Is(err, rpctypes.ErrLeaseNotFound) {
		return operationError(ctx, "revoke service lease", err)
	}
	return nil
}

type publication struct {
	registry    *Registry
	serviceName string
	instanceID  string
	records     []instanceRecord
	stopRenewal context.CancelFunc
	done        chan struct{}
	leaseID     clientv3.LeaseID
	withdrawn   atomic.Bool
}

func (instance *publication) renew(ctx context.Context) {
	defer close(instance.done)
	ticker := time.NewTicker(instance.registry.renewInterval)
	defer ticker.Stop()
	failed := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		err := instance.renewLease(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil && !failed {
			slog.WarnContext(ctx, "etcd service renewal failed", "event", "registry_renew_failed", "service", instance.serviceName, "instance_id", instance.instanceID, "error", err)
		}
		if err == nil && failed {
			slog.InfoContext(ctx, "etcd service renewal recovered", "event", "registry_renew_recovered", "service", instance.serviceName, "instance_id", instance.instanceID)
		}
		failed = err != nil
	}
}

func (instance *publication) renewLease(ctx context.Context) error {
	if instance.leaseID != 0 {
		renewContext, cancel := context.WithTimeout(ctx, instance.registry.requestTimeout)
		_, err := instance.registry.client.KeepAliveOnce(renewContext, instance.leaseID)
		if err == nil {
			cancel()
			return nil
		}
		if !errors.Is(err, rpctypes.ErrLeaseNotFound) {
			renewError := operationError(renewContext, "renew service lease", err)
			cancel()
			return renewError
		}
		cancel()
		slog.WarnContext(ctx, "etcd service lease lost", "event", "registry_lease_lost", "service", instance.serviceName, "instance_id", instance.instanceID)
	}
	leaseID, err := instance.registry.publishRecords(ctx, instance.records)
	instance.leaseID = leaseID
	if err == nil {
		slog.InfoContext(ctx, "etcd service republished", "event", "registry_republished", "service", instance.serviceName, "instance_id", instance.instanceID)
	}
	return err
}

func (instance *publication) withdraw(ctx context.Context) error {
	if instance.withdrawn.Load() {
		return nil
	}
	instance.stopRenewal()
	select {
	case <-instance.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := instance.registry.revoke(ctx, instance.leaseID); err != nil {
		return err
	}
	if instance.withdrawn.CompareAndSwap(false, true) {
		slog.InfoContext(ctx, "etcd service withdrawn", "event", "registry_withdrawn", "service", instance.serviceName, "instance_id", instance.instanceID)
	}
	return nil
}

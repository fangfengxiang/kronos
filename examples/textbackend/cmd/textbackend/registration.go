package main

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/fangfengxiang/kronos/framework/app"
	"github.com/fangfengxiang/kronos/infra/consul"
)

const registryServiceName = "textbackend"

type registeredBackend struct {
	app.Server
	registry   *consul.Registry
	instanceID string
}

func registerBackend(ctx context.Context, server app.Server, registry *consul.Registry, address string) (app.Server, error) {
	instance := consul.Instance{ID: registryServiceName + "-" + rand.Text(), Name: registryServiceName, Address: address}
	backend := registeredBackend{Server: server, registry: registry, instanceID: instance.ID}
	registerContext, cancelRegistration := context.WithTimeout(ctx, 2*time.Second)
	defer cancelRegistration()
	if err := registry.Register(registerContext, instance); err != nil {
		rollbackContext, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancelRollback()
		return nil, errors.Join(err, backend.Stop(rollbackContext))
	}
	return backend, nil
}

func (backend registeredBackend) Stop(ctx context.Context) error {
	withdrawContext, cancel := context.WithTimeout(ctx, time.Second)
	withdrawError := backend.registry.Deregister(withdrawContext, backend.instanceID)
	cancel()
	return errors.Join(withdrawError, backend.Server.Stop(ctx))
}

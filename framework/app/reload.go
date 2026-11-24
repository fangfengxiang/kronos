package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/cloudflare/tableflip"
)

// ReloadConfig controls replacement startup. OnError observes a failed reload
// while the current generation continues serving. It must return promptly.
type ReloadConfig struct {
	StartupTimeout time.Duration
	PIDFile        string
	OnError        func(error)
}

// RunWithReload runs the application with TCP listener inheritance. The caller
// owns the reload notifications and cancels ctx to shut down. A ready
// replacement also triggers graceful shutdown of the current generation.
// Only one reload-enabled App can run in a process.
func (application *App) RunWithReload(ctx context.Context, config ReloadConfig, requests <-chan os.Signal) error {
	return application.RunWithReloadAndReadyHook(ctx, config, requests, nil)
}

// RunWithReloadAndReadyHook publishes the inherited endpoints before confirming
// readiness to Tableflip. If publication fails, the previous process keeps
// serving. A successful publication is released on shutdown or failed handoff.
func (application *App) RunWithReloadAndReadyHook(ctx context.Context, config ReloadConfig, requests <-chan os.Signal, hook ReadyHook) error {
	if config.StartupTimeout <= 0 {
		return errors.New("reload startup timeout must be positive")
	}
	addresses := make(map[string]struct{}, len(application.bindings))
	for _, binding := range application.bindings {
		if _, used := addresses[binding.Address]; used {
			return fmt.Errorf("reload requires distinct listener addresses: %q is repeated", binding.Address)
		}
		addresses[binding.Address] = struct{}{}
	}
	if err := application.begin(ctx); err != nil {
		return err
	}
	upgrader, err := tableflip.New(tableflip.Options{
		UpgradeTimeout: config.StartupTimeout,
		PIDFile:        config.PIDFile,
	})
	if err != nil {
		return fmt.Errorf("initialize application reload: %w", err)
	}

	reloadContext, cancelReload := context.WithCancel(ctx)
	var stopOnCancellation func() bool
	ready := make(chan struct{})
	requestsDone := make(chan struct{})
	go func() {
		defer close(requestsDone)
		watchReloadRequests(reloadContext, ready, requests, upgrader, config.OnError)
	}()
	defer func() {
		cancelReload()
		if stopOnCancellation != nil {
			stopOnCancellation()
		}
		upgrader.Stop()
		<-upgrader.Exit()
		<-requestsDone
	}()

	var listenerConfig net.ListenConfig
	return application.run(ctx, runOptions{
		readyHook: hook,
		listen: func(ctx context.Context, network, address string) (net.Listener, error) {
			return upgrader.ListenWithCallback(network, address, func(network, address string) (net.Listener, error) {
				return listenerConfig.Listen(ctx, network, address)
			})
		},
		confirmReady: func() error {
			if err := upgrader.Ready(); err != nil {
				return err
			}
			stopOnCancellation = context.AfterFunc(reloadContext, upgrader.Stop)
			close(ready)
			return nil
		},
		onStopping: cancelReload,
		replaced:   upgrader.Exit(),
	})
}

func watchReloadRequests(ctx context.Context, ready <-chan struct{}, requests <-chan os.Signal, upgrader *tableflip.Upgrader, reportError func(error)) {
	select {
	case <-ctx.Done():
		return
	case <-ready:
	}
	if err := upgrader.WaitForParent(ctx); err != nil && ctx.Err() == nil && reportError != nil {
		reportError(fmt.Errorf("wait for previous process: %w", err))
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-upgrader.Exit():
			return
		case _, open := <-requests:
			if !open {
				return
			}
			if err := upgrader.Upgrade(); err != nil && ctx.Err() == nil && reportError != nil {
				reportError(fmt.Errorf("reload application: %w", err))
			}
		}
	}
}

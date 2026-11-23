// Package app coordinates the lifecycle of an explicitly assembled service.
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// ErrAlreadyRun indicates that the application has already been started.
var ErrAlreadyRun = errors.New("application can only run once")

// Config controls the time available for all transports to stop.
type Config struct {
	ShutdownTimeout time.Duration
}

// Server serves an acquired listener. Start blocks while serving. Stop can run
// before or concurrently with Start and must honor its context.
type Server interface {
	Start(context.Context) error
	Stop(context.Context) error
}

// Binding identifies a TCP listener and the function that registers a server
// on it. The application owns the listener. Build owns any other resources it
// acquires before returning.
type Binding struct {
	Name    string
	Address string
	Build   func(net.Listener) (Server, error)
}

// App owns its listeners and serving state. Construct it with New.
type App struct {
	shutdownTimeout time.Duration
	bindings        []Binding
	started         atomic.Bool
	ready           atomic.Bool
}

// New validates a set of bindings and prepares an application for Run.
func New(config Config, bindings ...Binding) (*App, error) {
	if config.ShutdownTimeout <= 0 {
		return nil, errors.New("application shutdown timeout must be positive")
	}
	if len(bindings) == 0 {
		return nil, errors.New("application requires at least one listener binding")
	}
	for _, binding := range bindings {
		if binding.Name == "" || binding.Address == "" || binding.Build == nil {
			return nil, fmt.Errorf("listener binding %q requires a name, address, and build function", binding.Name)
		}
	}
	return &App{shutdownTimeout: config.ShutdownTimeout, bindings: slices.Clone(bindings)}, nil
}

// Ready reports whether all listeners are serving and the application is still
// admitting new work.
func (application *App) Ready() bool {
	return application.ready.Load()
}

type boundServer struct {
	name     string
	listener net.Listener
	server   Server
}

type serveResult struct {
	name string
	err  error
}

// Run acquires all listeners before starting the servers. Cancellation requests
// a graceful stop without canceling requests already admitted.
func (application *App) Run(ctx context.Context) error {
	if err := application.begin(ctx); err != nil {
		return err
	}
	var listenerConfig net.ListenConfig
	return application.run(ctx, runOptions{listen: listenerConfig.Listen})
}

func (application *App) begin(ctx context.Context) error {
	if !application.started.CompareAndSwap(false, true) {
		return ErrAlreadyRun
	}
	return ctx.Err()
}

type runOptions struct {
	listen       func(context.Context, string, string) (net.Listener, error)
	confirmReady func() error
	onStopping   func()
	replaced     <-chan struct{}
}

func (application *App) run(ctx context.Context, options runOptions) (runError error) {
	servers := make([]boundServer, 0, len(application.bindings))
	servingStarted := false
	defer func() {
		if !servingStarted {
			runError = errors.Join(runError, application.stopPrepared(ctx, servers))
		}
		runError = errors.Join(runError, closeListeners(servers))
	}()

	for _, binding := range application.bindings {
		listener, err := options.listen(ctx, "tcp", binding.Address)
		if err != nil {
			return fmt.Errorf("listen for %s: %w", binding.Name, err)
		}
		servers = append(servers, boundServer{name: binding.Name, listener: listener})
	}

	accepts := make(chan struct{}, len(servers))
	allowAccept := make(chan struct{})
	stopAccept := make(chan struct{})
	for index, binding := range application.bindings {
		if err := ctx.Err(); err != nil {
			return err
		}
		listener := &readyListener{
			Listener: servers[index].listener,
			accepts:  accepts,
			allowed:  allowAccept,
			stopping: stopAccept,
			closed:   make(chan struct{}),
		}
		servers[index].listener = listener
		server, err := binding.Build(listener)
		if err != nil {
			return fmt.Errorf("build %s server: %w", binding.Name, err)
		}
		if server == nil {
			return fmt.Errorf("build %s server: server is nil", binding.Name)
		}
		servers[index].server = server
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	servingContext, cancelServing := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelServing()
	serveResults := make(chan serveResult, len(servers))
	servingStarted = true
	for _, endpoint := range servers {
		go func() {
			serveResults <- serveResult{name: endpoint.name, err: endpoint.server.Start(servingContext)}
		}()
	}

	publishReady := func() error {
		if options.confirmReady != nil {
			if err := options.confirmReady(); err != nil {
				return fmt.Errorf("confirm application readiness: %w", err)
			}
		}
		application.ready.Store(true)
		close(allowAccept)
		return nil
	}
	running, serveError := waitForStop(ctx, accepts, serveResults, len(servers), publishReady, options.replaced)
	application.ready.Store(false)
	close(stopAccept)
	if options.onStopping != nil {
		options.onStopping()
	}
	return errors.Join(serveError, application.shutdown(servingContext, cancelServing, servers, serveResults, running))
}

func waitForStop(ctx context.Context, accepts <-chan struct{}, results <-chan serveResult, serverCount int, publishReady func() error, replaced <-chan struct{}) (int, error) {
	for range serverCount {
		select {
		case <-ctx.Done():
			return serverCount, nil
		case result := <-results:
			return serverCount - 1, unexpectedExit(result)
		case <-accepts:
		}
	}

	select {
	case <-ctx.Done():
		return serverCount, nil
	case result := <-results:
		return serverCount - 1, unexpectedExit(result)
	default:
	}
	if err := publishReady(); err != nil {
		return serverCount, err
	}

	select {
	case <-ctx.Done():
		return serverCount, nil
	case <-replaced:
		return serverCount, nil
	case result := <-results:
		return serverCount - 1, unexpectedExit(result)
	}
}

func unexpectedExit(result serveResult) error {
	if result.err == nil {
		return fmt.Errorf("serve %s: server exited before shutdown", result.name)
	}
	return fmt.Errorf("serve %s: %w", result.name, result.err)
}

func (application *App) shutdown(ctx context.Context, cancelServing context.CancelFunc, servers []boundServer, serveResults <-chan serveResult, running int) error {
	shutdownContext, cancelShutdown := context.WithTimeout(ctx, application.shutdownTimeout)
	defer cancelShutdown()
	stopResults := make(chan serveResult, len(servers))
	for _, endpoint := range servers {
		go func() {
			stopResults <- serveResult{name: endpoint.name, err: endpoint.server.Stop(shutdownContext)}
		}()
	}

	var shutdownError error
	for stopping := len(servers); stopping > 0 || running > 0; {
		select {
		case <-shutdownContext.Done():
			cancelServing()
			return errors.Join(shutdownError, fmt.Errorf("application shutdown: %w", shutdownContext.Err()))
		case result := <-stopResults:
			stopping--
			if result.err != nil {
				shutdownError = errors.Join(shutdownError, fmt.Errorf("stop %s: %w", result.name, result.err))
			}
		case result := <-serveResults:
			running--
			if result.err != nil && !errors.Is(result.err, net.ErrClosed) {
				shutdownError = errors.Join(shutdownError, fmt.Errorf("serve %s: %w", result.name, result.err))
			}
		}
	}
	return errors.Join(shutdownError, shutdownContext.Err())
}

func (application *App) stopPrepared(ctx context.Context, servers []boundServer) error {
	rollbackContext, cancelRollback := context.WithTimeout(context.WithoutCancel(ctx), application.shutdownTimeout)
	defer cancelRollback()
	var rollbackError error
	for _, endpoint := range slices.Backward(servers) {
		if endpoint.server == nil {
			continue
		}
		if err := endpoint.server.Stop(rollbackContext); err != nil {
			rollbackError = errors.Join(rollbackError, fmt.Errorf("rollback %s: %w", endpoint.name, err))
		}
	}
	return errors.Join(rollbackError, rollbackContext.Err())
}

func closeListeners(servers []boundServer) error {
	var closeError error
	for _, endpoint := range slices.Backward(servers) {
		if err := endpoint.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			closeError = errors.Join(closeError, fmt.Errorf("close %s listener: %w", endpoint.name, err))
		}
	}
	return closeError
}

type readyListener struct {
	net.Listener
	firstAccept sync.Once
	closeOnce   sync.Once
	accepts     chan<- struct{}
	allowed     <-chan struct{}
	stopping    <-chan struct{}
	closed      chan struct{}
}

func (listener *readyListener) Accept() (net.Conn, error) {
	listener.firstAccept.Do(func() { listener.accepts <- struct{}{} })
	select {
	case <-listener.closed:
		return nil, net.ErrClosed
	case <-listener.stopping:
		return nil, net.ErrClosed
	case <-listener.allowed:
	}
	select {
	case <-listener.stopping:
		return nil, net.ErrClosed
	default:
	}
	return listener.Listener.Accept()
}

func (listener *readyListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.closed) })
	return listener.Listener.Close()
}

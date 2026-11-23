package kronos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fangfengxiang/kronos/framework/app"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/platform/kronos/internal/transport"
	"google.golang.org/protobuf/proto"
)

// App assembles the listeners for a set of Protobuf services.
type App struct {
	config   settings
	mu       sync.Mutex
	started  bool
	services []transport.Service
	inFlight atomic.Int64
}

// New checks the application's name, listeners and time budget.
func New(options ...Option) (*App, error) {
	config, err := applyOptions(options)
	if err != nil {
		return nil, err
	}
	if config.name == "" || strings.TrimSpace(config.name) != config.name {
		return nil, errors.New("Kronos requires a service name")
	}
	if config.grpcAddress == "" && config.microRPCAddress == "" {
		return nil, errors.New("Kronos requires at least one listener")
	}
	if config.shutdownTimeout <= 0 {
		return nil, errors.New("Kronos shutdown timeout must be positive")
	}
	if !validHost(config.advertiseHost) {
		return nil, errors.New("Kronos advertise host must be an IP address or hostname")
	}
	return &App{config: config}, nil
}

// Register binds a Protobuf service before Run starts.
func (application *App) Register(definition Service) error {
	service, err := definition.prepare()
	if err != nil {
		return err
	}
	application.mu.Lock()
	defer application.mu.Unlock()
	if application.started {
		return app.ErrAlreadyRun
	}
	for _, registered := range application.services {
		if registered.Name == service.Name {
			return fmt.Errorf("Kronos already registers %q", service.Name)
		}
	}
	application.services = append(application.services, service)
	return nil
}

// Run starts the configured listeners and blocks until shutdown completes.
func (application *App) Run(ctx context.Context) error {
	application.mu.Lock()
	if application.started {
		application.mu.Unlock()
		return app.ErrAlreadyRun
	}
	application.started = true
	services := slices.Clone(application.services)
	application.mu.Unlock()
	if len(services) == 0 {
		return errors.New("Kronos requires at least one registered service")
	}

	var engine *app.App
	for i, service := range services {
		service.Methods = slices.Clone(service.Methods)
		for j, method := range service.Methods {
			service.Methods[j].Invoke = application.observe(service.Name+"/"+method.Name, func() bool { return engine.Ready() }, method.Invoke)
		}
		services[i] = service
	}
	config := application.config
	var bindings []app.Binding
	if config.grpcAddress != "" {
		bindings = append(bindings, app.Binding{Name: string(rpc.ProtocolGRPC), Address: config.grpcAddress,
			Build: func(listener net.Listener) (app.Server, error) { return transport.GRPCServer(listener, services) }})
	}
	if config.microRPCAddress != "" {
		bindings = append(bindings, app.Binding{Name: string(rpc.ProtocolMicroV2), Address: config.microRPCAddress,
			Build: func(listener net.Listener) (app.Server, error) {
				return transport.MicroRPCServer(listener, config.name, services)
			}})
	}

	engine, err := app.New(app.Config{ShutdownTimeout: config.shutdownTimeout}, bindings...)
	if err != nil {
		return err
	}
	if config.registry == nil {
		return engine.Run(ctx)
	}
	return engine.RunWithReadyHook(ctx, application.publish)
}

func (application *App) publish(ctx context.Context, endpoints []app.Endpoint) (func(context.Context) error, error) {
	var grpcAddress, microAddress string
	for _, endpoint := range endpoints {
		host, port, err := net.SplitHostPort(endpoint.Address)
		if err != nil {
			return nil, err
		}
		if application.config.advertiseHost != "" {
			host = application.config.advertiseHost
		} else if ip, err := netip.ParseAddr(host); err == nil && ip.IsUnspecified() {
			host = "127.0.0.1"
			if ip.Is6() {
				host = "::1"
			}
		}
		switch endpoint.Name {
		case string(rpc.ProtocolGRPC):
			grpcAddress = net.JoinHostPort(host, port)
		case string(rpc.ProtocolMicroV2):
			microAddress = net.JoinHostPort(host, port)
		}
	}
	return application.config.registry.Publish(ctx, application.config.name, grpcAddress, microAddress)
}

type unaryCall func(context.Context, proto.Message) (proto.Message, error)

func (application *App) observe(operation string, ready func() bool, invoke unaryCall) unaryCall {
	return func(ctx context.Context, request proto.Message) (proto.Message, error) {
		if !ready() {
			return nil, &rpc.Error{Code: rpc.Unavailable, Reason: rpc.ServiceUnavailableReason, Message: "service is unavailable"}
		}
		started := time.Now()
		application.inFlight.Add(1)
		response, err := invoke(ctx, request)
		remaining := application.inFlight.Add(-1)
		level := slog.LevelDebug
		if err != nil {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "RPC request completed", "event", "rpc_finished", "operation", operation,
			"failed", err != nil, "duration_us", time.Since(started).Microseconds(), "in_flight", remaining)
		return response, err
	}
}

func validHost(host string) bool {
	if ip, err := netip.ParseAddr(host); err == nil {
		return !ip.Unmap().IsUnspecified()
	}
	return !strings.ContainsAny(host, " :/\\[]\t\r\n@?#*")
}

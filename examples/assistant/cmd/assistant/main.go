package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/app"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/blades"
)

func main() {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stopSignals()
	if err != nil {
		log.Printf("assistant: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("assistant", flag.ContinueOnError)
	configPath := flags.String(configPathFlag, "", "startup JSON config file")
	httpAddress := flags.String("http", "127.0.0.1:8080", "HTTP listen address")
	grpcAddress := flags.String("grpc", "127.0.0.1:9090", "gRPC listen address")
	sseAddress := flags.String("sse", "", "SSE listen address (empty disables SSE)")
	microV2Address := flags.String("micro-v2", "", "go-micro v2 listen address (empty disables compatibility)")
	shutdownTimeout := flags.Duration("shutdown-timeout", 5*time.Second, "time allowed for graceful shutdown")
	reloadPIDFile := flags.String(reloadPIDFileFlag, "", "PID file for SIGHUP reload (empty disables reload)")
	reloadStartupTimeout := flags.Duration(reloadTimeoutFlag, 30*time.Second, "time allowed for a replacement to become ready")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *configPath != "" {
		if err := applyConfigFile(flags, *configPath); err != nil {
			return err
		}
	}

	// Run builds the bindings after application has been assigned.
	var application *app.App
	runner, err := blades.New("assistant", echoText)
	if err != nil {
		return err
	}
	agent, err := newAgentService(runner)
	if err != nil {
		return err
	}
	service := admittingService{
		ready:  func() bool { return application.Ready() },
		next:   agent,
		stream: agent,
	}
	bindings := []app.Binding{
		{
			Name:    "HTTP",
			Address: *httpAddress,
			Build: func(listener net.Listener) (app.Server, error) {
				return assistant.NewHTTPServerWithProbes(listener, service, application.Probes()), nil
			},
		},
		{
			Name:    "gRPC",
			Address: *grpcAddress,
			Build: func(listener net.Listener) (app.Server, error) {
				return assistant.NewGRPCServerWithStream(listener, service, service), nil
			},
		},
	}
	if *sseAddress != "" {
		bindings = append(bindings, app.Binding{Name: "SSE", Address: *sseAddress, Build: func(listener net.Listener) (app.Server, error) {
			return assistant.NewSSEServer(listener, service), nil
		}})
	}
	if *microV2Address != "" {
		bindings = append(bindings, app.Binding{Name: "go-micro v2", Address: *microV2Address, Build: func(listener net.Listener) (app.Server, error) {
			return assistant.NewMicroV2Server(listener, service), nil
		}})
	}
	application, err = app.New(app.Config{ShutdownTimeout: *shutdownTimeout}, bindings...)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		log.Printf("starting assistant %s listener at %s", binding.Name, binding.Address)
	}
	if *reloadPIDFile != "" {
		reloadSignals := make(chan os.Signal, 1)
		signal.Notify(reloadSignals, syscall.SIGHUP)
		defer signal.Stop(reloadSignals)
		return application.RunWithReload(ctx, app.ReloadConfig{
			StartupTimeout: *reloadStartupTimeout,
			PIDFile:        *reloadPIDFile,
			OnError:        func(err error) { log.Print(err) },
		}, reloadSignals)
	}
	return application.Run(ctx)
}

type admittingService struct {
	ready  func() bool
	next   assistant.Service
	stream assistant.StreamService
}

func (service admittingService) Run(ctx context.Context, request *assistant.RunRequest) (*assistant.RunResponse, error) {
	if err := service.checkReadiness(); err != nil {
		return nil, err
	}
	return service.next.Run(ctx, request)
}

func (service admittingService) StreamRun(ctx context.Context, request *assistant.StreamRunRequest, emit func(string) error) error {
	if err := service.checkReadiness(); err != nil {
		return err
	}
	return service.stream.StreamRun(ctx, request, emit)
}

func (service admittingService) checkReadiness() error {
	if !service.ready() {
		return &rpc.Error{Code: rpc.Unavailable, Reason: rpc.ServiceUnavailableReason, Message: "service is unavailable"}
	}
	return nil
}

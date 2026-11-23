package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/framework/app"
	"github.com/fangfengxiang/kronos/framework/log"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/infra/consul"
)

func main() {
	logger := log.NewJSON(os.Stderr, nil).With("service", "text-backend")
	slog.SetDefault(logger)
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stopSignals()
	if err != nil {
		logger.ErrorContext(ctx, "text backend failed", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) (runError error) {
	flags := flag.NewFlagSet("textbackend", flag.ContinueOnError)
	grpcAddress := flags.String("grpc", "127.0.0.1:9290", "gRPC listen address")
	consulEndpoint := flags.String("consul", "", "Consul HTTP(S) agent URL (empty disables registration)")
	advertiseAddress := flags.String("advertise-grpc", "", "advertised gRPC host:port (defaults to the bound address)")
	profileName := flags.String("mysql-profile", "", "text profile name in MySQL (empty uses the built-in profile)")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	var registry *consul.Registry
	if *consulEndpoint != "" {
		var err error
		registry, err = consul.New(*consulEndpoint)
		if err != nil {
			return err
		}
		defer registry.Close()
	} else if *advertiseAddress != "" {
		return errors.New("advertise-grpc requires a Consul agent URL")
	}
	service := textService{}
	if *profileName != "" {
		profile, err := openMySQLProfile(ctx, *profileName, os.Getenv("KRONOS_TEXTBACKEND_MYSQL_DSN"))
		if err != nil {
			return err
		}
		defer func() { runError = errors.Join(runError, profile.pool.Close()) }()
		service.prefixSource = profile
	}
	application, err := app.New(app.Config{ShutdownTimeout: 5 * time.Second}, app.Binding{
		Name:    "gRPC",
		Address: *grpcAddress,
		Build: func(listener net.Listener) (app.Server, error) {
			server, err := textbackend.NewGRPCServer(listener, service)
			if err != nil || registry == nil {
				return server, err
			}
			address := *advertiseAddress
			if address == "" {
				address = listener.Addr().String()
			}
			return registerBackend(ctx, server, registry, address)
		},
	})
	if err != nil {
		return err
	}
	return application.Run(ctx)
}

type prefixSource interface {
	Prefix(context.Context) (string, error)
}

type textService struct {
	prefixSource prefixSource
}

func (service textService) Generate(ctx context.Context, request *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prompt := request.GetPrompt()
	if strings.TrimSpace(prompt) == "" {
		return nil, &rpc.Error{
			Code:    rpc.InvalidArgument,
			Reason:  textbackend.ErrorReason_ERROR_REASON_INVALID_PROMPT.String(),
			Message: "prompt must not be blank",
		}
	}
	prefix := "backend: "
	if service.prefixSource != nil {
		var err error
		prefix, err = service.prefixSource.Prefix(ctx)
		if err != nil {
			return nil, err
		}
	}
	return &textbackend.GenerateResponse{Text: prefix + prompt}, nil
}

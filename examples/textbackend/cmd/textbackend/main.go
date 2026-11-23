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

func run(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("textbackend", flag.ContinueOnError)
	grpcAddress := flags.String("grpc", "127.0.0.1:9290", "gRPC listen address")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	application, err := app.New(app.Config{ShutdownTimeout: 5 * time.Second}, app.Binding{
		Name:    "gRPC",
		Address: *grpcAddress,
		Build: func(listener net.Listener) (app.Server, error) {
			return textbackend.NewGRPCServer(listener, textService{})
		},
	})
	if err != nil {
		return err
	}
	return application.Run(ctx)
}

type textService struct{}

func (textService) Generate(ctx context.Context, request *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
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
	return &textbackend.GenerateResponse{Text: "backend: " + prompt}, nil
}

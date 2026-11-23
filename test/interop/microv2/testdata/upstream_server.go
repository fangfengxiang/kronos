package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	memorybroker "github.com/micro/go-micro/v2/broker/memory"
	microerrors "github.com/micro/go-micro/v2/errors"
	memoryregistry "github.com/micro/go-micro/v2/registry/memory"
	"github.com/micro/go-micro/v2/server"
)

type Assistant struct{}

func (*Assistant) Run(_ context.Context, request *Request, response *Response) error {
	if request.Prompt == "" {
		return microerrors.BadRequest("ERROR_REASON_INVALID_PROMPT", "prompt must not be blank")
	}
	response.Text = request.Prompt
	return nil
}

func serve(readyFile string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	legacyServer := server.NewServer(server.Address("127.0.0.1:0"), server.Name("kronos.assistant.v1.Assistant"),
		server.Broker(memorybroker.NewBroker()), server.Registry(memoryregistry.NewRegistry()))
	if err := legacyServer.Handle(legacyServer.NewHandler(&Assistant{})); err != nil {
		return err
	}
	if err := legacyServer.Start(); err != nil {
		return err
	}
	defer legacyServer.Stop()
	if err := os.WriteFile(readyFile+".tmp", []byte(legacyServer.Options().Address), 0600); err != nil {
		return err
	}
	if err := os.Rename(readyFile+".tmp", readyFile); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

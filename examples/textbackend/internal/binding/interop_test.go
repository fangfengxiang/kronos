package binding_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	wire "github.com/fangfengxiang/kronos/examples/textbackend/internal/wire/kronos/textbackend/v1"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestKronosAndStandardGRPCClients(t *testing.T) {
	const invalidPrompt = "ERROR_REASON_INVALID_PROMPT"
	deadlineObserved := make(chan error, 1)
	endpoint := startBindingServer(t, generateFunc(func(ctx context.Context, input *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
		switch input.GetPrompt() {
		case "":
			return nil, &rpc.Error{Code: rpc.InvalidArgument, Reason: invalidPrompt, Message: "missing prompt"}
		case "wait":
			<-ctx.Done()
			deadlineObserved <- ctx.Err()
			return nil, ctx.Err()
		case "fail":
			return nil, errors.New("private backend detail")
		case "panic":
			panic("private backend panic")
		}
		return &textbackend.GenerateResponse{Text: input.GetPrompt() + " " + request.ID(ctx)}, nil
	}))
	client, err := textbackend.NewGRPCClient(t.Context(), endpoint, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	standard := wire.NewTextBackendServiceClient(connection)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	const requestID = "reference-client_01"
	standardContext := metadata.AppendToOutgoingContext(ctx, request.IDHeader, requestID)
	input := &textbackend.GenerateRequest{Prompt: "你好"}
	generated, err := standard.Generate(standardContext, input)
	if err != nil || generated.GetText() != "你好 "+requestID {
		t.Fatalf("standard client = %v, %v", generated, err)
	}
	result, err := client.Generate(request.WithID(ctx, requestID), input)
	if err != nil || result.GetText() != generated.GetText() {
		t.Fatalf("Kronos client = %v, %v", result, err)
	}
	_, err = standard.Generate(ctx, &wire.GenerateRequest{})
	standardFailure := status.Convert(err)
	details := standardFailure.Details()
	if standardFailure.Code() != codes.InvalidArgument || len(details) != 1 {
		t.Fatalf("standard invalid prompt = %v", err)
	}
	detail, ok := details[0].(*errdetails.ErrorInfo)
	if !ok || detail.GetReason() != invalidPrompt {
		t.Fatalf("standard error details = %v", details)
	}
	_, err = client.Generate(ctx, &textbackend.GenerateRequest{})
	var failure *rpc.Error
	if !errors.As(err, &failure) || failure.Code != rpc.InvalidArgument || failure.Reason != invalidPrompt || failure.Message != standardFailure.Message() {
		t.Fatalf("Kronos invalid prompt = %v", err)
	}
	_, err = client.Generate(ctx, &textbackend.GenerateRequest{Prompt: "fail"})
	if !errors.As(err, &failure) || failure.Code != rpc.Internal || failure.Reason != textbackend.ErrorReason_ERROR_REASON_GENERATION_FAILED.String() || failure.Message != "text generation failed" {
		t.Fatalf("Kronos backend failure = %v", err)
	}
	_, err = standard.Generate(ctx, &wire.GenerateRequest{Prompt: "panic"})
	standardFailure = status.Convert(err)
	details = standardFailure.Details()
	if standardFailure.Code() != codes.Internal || standardFailure.Message() != "text generation failed" || len(details) != 1 {
		t.Fatalf("standard recovered backend failure = %v", err)
	}
	if detail, ok := details[0].(*errdetails.ErrorInfo); !ok || detail.GetReason() != textbackend.ErrorReason_ERROR_REASON_GENERATION_FAILED.String() {
		t.Fatalf("standard recovered backend error details = %v", details)
	}
	if result, err := standard.Generate(standardContext, input); err != nil || result.GetText() != generated.GetText() {
		t.Fatalf("standard backend after recovered panic = %v, %v", result, err)
	}
	if _, err := client.Generate(t.Context(), &textbackend.GenerateRequest{Prompt: "wait"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Kronos default deadline = %v", err)
	}
	select {
	case err := <-deadlineObserved:
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Fatalf("server context = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("default deadline did not reach the backend")
	}
	for range 2 {
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Generate(ctx, input); !errors.Is(err, rpc.ErrClientClosed) {
		t.Fatalf("Kronos closed connection = %v", err)
	}
}

type generateFunc func(context.Context, *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error)

func (generate generateFunc) Generate(ctx context.Context, input *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error) {
	return generate(ctx, input)
}

func startBindingServer(t *testing.T, service textbackend.TextBackendServiceUnaryService) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	server, err := textbackend.NewGRPCServer(listener, service)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Start(context.WithoutCancel(t.Context())) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Stop(ctx); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	return listener.Addr().String()
}

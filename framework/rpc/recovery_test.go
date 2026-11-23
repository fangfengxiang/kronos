package rpc_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/fangfengxiang/kronos/framework/rpc"
)

func TestRecoveryUsesSafeDiagnostics(t *testing.T) {
	t.Setenv("GODEBUG", "panicnil=0")
	var diagnostics bytes.Buffer
	setRecoveryLogger(t, slog.NewJSONHandler(&diagnostics, nil))
	const privateRequest = "private request value"
	const privatePanic = "private panic value"
	request := privateRequest
	for _, panicValue := range []any{privatePanic, nil} {
		handler := rpc.RecoverUnary(func(context.Context, *string) (*string, error) {
			panic(panicValue)
		})
		response, err := handler(t.Context(), &request)
		if response != nil || !errors.Is(err, rpc.ErrHandlerPanic) {
			t.Fatalf("recovered failure = %v, %v", response, err)
		}
	}
	logged := diagnostics.String()
	if strings.Contains(logged, privateRequest) || strings.Contains(logged, privatePanic) ||
		!strings.Contains(logged, `"panic_type":"string"`) || !strings.Contains(logged, `"panic_type":"*runtime.PanicNilError"`) ||
		!strings.Contains(logged, "TestRecoveryUsesSafeDiagnostics") {
		t.Fatalf("unexpected recovery diagnostics: %s", logged)
	}
}

func TestRecoveryKeepsHandlerErrorAndIsolatesLoggerPanic(t *testing.T) {
	setRecoveryLogger(t, panickingLogHandler{Handler: slog.NewJSONHandler(io.Discard, nil)})
	providerFailure := errors.New("provider failed")
	input := "你好"
	callContext := t.Context()
	unary := rpc.RecoverUnary(func(ctx context.Context, request *string) (*string, error) {
		if ctx != callContext || request != &input {
			t.Error("unary handler received different arguments")
		}
		return request, providerFailure
	})
	if response, err := unary(callContext, &input); response != &input || err != providerFailure {
		t.Fatalf("normal handler returned %v, %v", response, err)
	}
	stream := rpc.RecoverStream(func(ctx context.Context, request *string, emit func(string) error) error {
		if err := emit(*request); err != nil {
			return err
		}
		panic("stream failed")
	})
	var delivered []string
	if err := stream(callContext, &input, func(event string) error {
		delivered = append(delivered, event)
		return nil
	}); err != rpc.ErrHandlerPanic || len(delivered) != 1 || delivered[0] != input {
		t.Fatalf("stream returned %v after %q", err, delivered)
	}
	if rpc.RecoverStream[string, string](nil) != nil || rpc.RecoverUnary[string, string](nil) != nil {
		t.Fatal("absent handlers must remain absent")
	}
}

type panickingLogHandler struct{ slog.Handler }

func (panickingLogHandler) Handle(context.Context, slog.Record) error {
	panic("logger failed")
}

func setRecoveryLogger(t *testing.T, handler slog.Handler) {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })
}

func ExampleRecoverUnary() {
	handler := rpc.RecoverUnary(func(_ context.Context, message *string) (*string, error) {
		response := "got " + *message
		return &response, nil
	})
	input := "hello"
	response, err := handler(context.Background(), &input)
	if err != nil {
		panic(err)
	}
	fmt.Println(*response)
	// Output: got hello
}

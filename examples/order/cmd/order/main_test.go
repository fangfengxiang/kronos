package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/examples/order"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

func TestMetricsStartupErrorPreservesCause(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	rejected := errors.New("metrics setup failed")
	observer := &rejectingMetrics{err: rejected}
	application, err := kronos.New(kronos.Name(order.OrderServiceName),
		kronos.GRPC(occupied.Addr().String()), kronos.Metrics(observer))
	if err != nil {
		t.Fatal(err)
	}
	if err := application.Register(order.BindOrderService(order.NewService())); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := application.Run(ctx); !errors.Is(err, rejected) {
		t.Fatalf("Run returned %v, want registration error", err)
	}
	if want := []string{"/" + order.OrderServiceName + "/GetOrder"}; !slices.Equal(observer.operations, want) {
		t.Errorf("registered methods = %v, want %v", observer.operations, want)
	}
}

type rejectingMetrics struct {
	err        error
	operations []string
}

func (metrics *rejectingMetrics) RegisterRPCOperations(operations ...string) error {
	metrics.operations = slices.Clone(operations)
	return metrics.err
}

func (*rejectingMetrics) BeginRPC(string, string, string) func(bool, time.Duration) {
	return nil
}

func TestCoreAssemblyFitsThirtyLines(t *testing.T) {
	files := token.NewFileSet()
	source, err := parser.ParseFile(files, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range source.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "run" {
			continue
		}
		lines := files.Position(function.End()).Line - files.Position(function.Pos()).Line + 1
		if lines > 30 {
			t.Fatalf("Order assembly uses %d lines, want at most 30", lines)
		}
		return
	}
	t.Fatal("Order assembly function was not found")
}

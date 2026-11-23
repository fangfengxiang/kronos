package runtime_test

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	kruntime "github.com/fangfengxiang/kronos/core/runtime"
)

func receive[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("waiting for goroutine callback: %v", ctx.Err())
		var zero T
		return zero
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestGoReportsReturn(t *testing.T) {
	ctx := testContext(t)
	ran := make(chan struct{})
	observed := make(chan kruntime.Result, 1)
	unexpected := make(chan string, 2)

	kruntime.Go(func() { close(ran) },
		nil,
		kruntime.WithLogger(func(kruntime.PanicInfo) { unexpected <- "logger" }),
		kruntime.WithPanicHandler(func(kruntime.PanicInfo) { unexpected <- "panic handler" }),
		kruntime.WithObserver(func(result kruntime.Result) { observed <- result }),
	)

	result := receive(t, ctx, observed)
	if result.Outcome != kruntime.Returned || result.Panic != nil || result.Duration < 0 {
		t.Fatalf("returned result = %+v", result)
	}
	select {
	case <-ran:
	default:
		t.Fatal("observer ran before the task returned")
	}
	select {
	case callback := <-unexpected:
		t.Fatalf("normal return called the %s", callback)
	default:
	}
}

func panicInTask() { panic("task panic") }

func TestGoReportsOriginalPanicAndStackInOrder(t *testing.T) {
	ctx := testContext(t)
	calls := make(chan string, 3)
	logged := make(chan kruntime.PanicInfo, 1)
	handled := make(chan kruntime.PanicInfo, 1)
	observed := make(chan kruntime.Result, 1)

	kruntime.Go(panicInTask,
		kruntime.WithLogger(func(info kruntime.PanicInfo) {
			calls <- "logger"
			logged <- info
		}),
		kruntime.WithPanicHandler(func(info kruntime.PanicInfo) {
			calls <- "handler"
			handled <- info
		}),
		kruntime.WithObserver(func(result kruntime.Result) {
			calls <- "observer"
			observed <- result
		}),
	)

	result := receive(t, ctx, observed)
	if result.Outcome != kruntime.Panicked || result.Panic == nil || result.Duration < 0 {
		t.Fatalf("panicked result = %+v", result)
	}
	for _, want := range []string{"logger", "handler", "observer"} {
		if got := receive(t, ctx, calls); got != want {
			t.Errorf("callback = %q, want %q", got, want)
		}
	}
	for _, info := range []kruntime.PanicInfo{receive(t, ctx, logged), receive(t, ctx, handled), *result.Panic} {
		if info.Value != "task panic" || !strings.Contains(info.Stack, "panicInTask") {
			t.Errorf("panic metadata lost original value or stack: %+v", info)
		}
	}
}

func TestGoIsolatesCallbackPanics(t *testing.T) {
	ctx := testContext(t)
	calls := make(chan string, 2)
	observed := make(chan kruntime.Result, 1)

	kruntime.Go(panicInTask,
		kruntime.WithLogger(func(kruntime.PanicInfo) {
			calls <- "logger"
			panic("logger panic")
		}),
		kruntime.WithPanicHandler(func(kruntime.PanicInfo) {
			calls <- "handler"
			panic("handler panic")
		}),
		kruntime.WithObserver(func(result kruntime.Result) { observed <- result }),
	)

	result := receive(t, ctx, observed)
	if result.Outcome != kruntime.Panicked || result.Panic == nil || result.Panic.Value != "task panic" {
		t.Fatalf("callback panics changed original result: %+v", result)
	}
	for _, want := range []string{"logger", "handler"} {
		if got := receive(t, ctx, calls); got != want {
			t.Errorf("callback = %q, want %q", got, want)
		}
	}
}

func TestGoKeepsOptionsLocalToEachTask(t *testing.T) {
	ctx := testContext(t)
	start := make(chan struct{})
	first := make(chan any, 1)
	second := make(chan any, 1)

	kruntime.Go(func() { <-start; panic("first") },
		kruntime.WithPanicHandler(func(info kruntime.PanicInfo) { first <- info.Value }))
	kruntime.Go(func() { <-start; panic("second") },
		kruntime.WithPanicHandler(func(info kruntime.PanicInfo) { second <- info.Value }))
	close(start)

	if got := receive(t, ctx, first); got != "first" {
		t.Errorf("first handler received %v", got)
	}
	if got := receive(t, ctx, second); got != "second" {
		t.Errorf("second handler received %v", got)
	}
}

func TestGoDistinguishesGoexit(t *testing.T) {
	ctx := testContext(t)
	observed := make(chan kruntime.Result, 1)
	unexpected := make(chan string, 2)

	kruntime.Go(runtime.Goexit,
		kruntime.WithLogger(func(kruntime.PanicInfo) { unexpected <- "logger" }),
		kruntime.WithPanicHandler(func(kruntime.PanicInfo) { unexpected <- "panic handler" }),
		kruntime.WithObserver(func(result kruntime.Result) { observed <- result }),
	)

	result := receive(t, ctx, observed)
	if result.Outcome != kruntime.Exited || result.Panic != nil || result.Duration < 0 {
		t.Fatalf("exited result = %+v", result)
	}
	select {
	case callback := <-unexpected:
		t.Fatalf("Goexit called the %s", callback)
	default:
	}
}

func TestGoReportsPanicAfterCallbacksExit(t *testing.T) {
	ctx := testContext(t)
	calls := make(chan string, 3)
	handled := make(chan kruntime.PanicInfo, 1)
	observed := make(chan kruntime.Result, 1)

	kruntime.Go(panicInTask,
		kruntime.WithLogger(func(kruntime.PanicInfo) {
			calls <- "logger"
			runtime.Goexit()
		}),
		kruntime.WithPanicHandler(func(info kruntime.PanicInfo) {
			calls <- "handler"
			handled <- info
			runtime.Goexit()
		}),
		kruntime.WithObserver(func(result kruntime.Result) {
			calls <- "observer"
			observed <- result
		}),
	)

	result := receive(t, ctx, observed)
	if result.Outcome != kruntime.Panicked || result.Panic == nil {
		t.Fatalf("observed result = %+v, want task panic", result)
	}
	for _, want := range []string{"logger", "handler", "observer"} {
		if got := receive(t, ctx, calls); got != want {
			t.Errorf("callback = %q, want %q", got, want)
		}
	}
	for _, info := range []kruntime.PanicInfo{receive(t, ctx, handled), *result.Panic} {
		if info.Value != "task panic" || !strings.Contains(info.Stack, "panicInTask") {
			t.Errorf("callback exit changed original panic metadata: %+v", info)
		}
	}
}

func TestGoReportsNilTaskAsPanic(t *testing.T) {
	ctx := testContext(t)
	observed := make(chan kruntime.Result, 1)

	kruntime.Go(nil,
		kruntime.WithLogger(func(kruntime.PanicInfo) {}),
		kruntime.WithObserver(func(result kruntime.Result) { observed <- result }),
	)

	result := receive(t, ctx, observed)
	if result.Outcome != kruntime.Panicked || result.Panic == nil {
		t.Fatalf("nil task result = %+v, want task panic", result)
	}
	if _, ok := result.Panic.Value.(runtime.Error); !ok {
		t.Errorf("nil task panic type = %T, want runtime.Error", result.Panic.Value)
	}
}

func TestGoReportsNilPanicWithDefaultSemantics(t *testing.T) {
	t.Setenv("GODEBUG", "panicnil=0")
	ctx := testContext(t)
	observed := make(chan kruntime.Result, 1)

	kruntime.Go(func() { panic(nil) },
		kruntime.WithLogger(func(kruntime.PanicInfo) {}),
		kruntime.WithObserver(func(result kruntime.Result) { observed <- result }),
	)

	result := receive(t, ctx, observed)
	if result.Outcome != kruntime.Panicked || result.Panic == nil {
		t.Fatalf("nil panic result = %+v, want task panic", result)
	}
	if _, ok := result.Panic.Value.(*runtime.PanicNilError); !ok {
		t.Errorf("nil panic type = %T, want *runtime.PanicNilError", result.Panic.Value)
	}
}

const privatePanicValue = "private-panic-payload-36fb8c"

func panicWithPrivateValue() { panic(privatePanicValue) }

func TestGoDefaultLogOmitsPanicValue(t *testing.T) {
	ctx := testContext(t)
	const childEnv = "KRONOS_RUNTIME_TEST_LOG_CHILD"
	if os.Getenv(childEnv) == "1" {
		observed := make(chan kruntime.Result, 1)
		kruntime.Go(panicWithPrivateValue,
			kruntime.WithObserver(func(result kruntime.Result) { observed <- result }),
		)
		result := receive(t, ctx, observed)
		if result.Outcome != kruntime.Panicked || result.Panic == nil || result.Panic.Value != privatePanicValue {
			t.Fatal("observer did not receive the original panic")
		}
		return
	}

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGoDefaultLogOmitsPanicValue$")
	cmd.Env = append(cmd.Environ(), childEnv+"=1")
	bytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("logging subprocess failed: %v\n%s", err, bytes)
	}
	output := string(bytes)
	if !strings.Contains(output, "task panic (string)") || !strings.Contains(output, "panicWithPrivateValue") {
		t.Fatalf("default log did not report the panic type and stack:\n%s", output)
	}
	if strings.Contains(output, privatePanicValue) {
		t.Fatal("default log printed the original panic value")
	}
}

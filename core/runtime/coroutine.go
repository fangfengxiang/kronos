// Package runtime provides goroutine primitives for Kronos.
package runtime

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"
)

// Outcome describes how a goroutine's task stopped.
type Outcome uint8

const (
	// Exited indicates that the task stopped without returning or panicking.
	Exited Outcome = iota
	// Returned indicates that the task returned normally.
	Returned
	// Panicked indicates that the task panicked and Go recovered it.
	Panicked
)

// PanicInfo contains the recovered value and the stack of its goroutine.
type PanicInfo struct {
	Value any
	Stack string
}

// Result records the task's outcome and elapsed time before reporting.
type Result struct {
	Outcome  Outcome
	Duration time.Duration
	Panic    *PanicInfo
}

type options struct {
	onPanic func(PanicInfo)
	log     func(PanicInfo)
	observe func(Result)
}

// Option configures one Go invocation.
type Option func(*options)

// WithPanicHandler sets the application callback for a recovered panic.
func WithPanicHandler(fn func(PanicInfo)) Option {
	return func(o *options) { o.onPanic = fn }
}

// WithLogger sets the panic logger for an invocation.
func WithLogger(fn func(PanicInfo)) Option {
	return func(o *options) { o.log = fn }
}

// WithObserver sets the callback for the task's final outcome.
func WithObserver(fn func(Result)) Option {
	return func(o *options) { o.observe = fn }
}

// Go runs fn in a new goroutine and reports failures through optional callbacks.
// Callbacks run synchronously in that goroutine and may run concurrently with
// callbacks from other Go invocations.
func Go(fn func(), opts ...Option) {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	go func() {
		start := time.Now()
		outcome := Exited
		defer func() {
			value := recover()
			result := Result{Outcome: outcome, Duration: time.Since(start)}
			// Independent defers keep later callbacks reachable during Goexit.
			if o.observe != nil {
				defer runCallback("observer", func() { o.observe(result) })
			}
			if value == nil {
				return
			}

			info := PanicInfo{Value: value, Stack: string(debug.Stack())}
			result.Outcome = Panicked
			result.Panic = &info
			if o.onPanic != nil {
				defer runCallback("panic handler", func() { o.onPanic(info) })
			}
			if o.log == nil {
				reportPanic("task panic", info.Value, info.Stack)
				return
			}

			logged := false
			defer func() {
				if !logged {
					reportPanic("task panic", info.Value, info.Stack)
				}
			}()
			logged = runCallback("logger", func() { o.log(info) })
		}()
		fn()
		outcome = Returned
	}()
}

func runCallback(name string, callback func()) (completed bool) {
	defer func() {
		if value := recover(); value != nil {
			reportPanic(name+" callback panic", value, string(debug.Stack()))
		}
	}()
	callback()
	return true
}

func reportPanic(message string, value any, stack string) {
	fmt.Fprintf(os.Stderr, "kronos runtime: %s (%T)\n%s", message, value, stack)
}

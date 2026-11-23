package blades_test

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/blades"
)

func TestRunAndStream(t *testing.T) {
	runner, err := blades.New("assistant", func(_ context.Context, prompt string) iter.Seq2[string, error] {
		return func(yield func(string, error) bool) {
			if yield("收到", nil) {
				yield(prompt, nil)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	const prompt = " 你好 🌍 "
	var deltas []string
	if err := runner.Stream(t.Context(), prompt, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	}); err != nil || !slices.Equal(deltas, []string{"收到", prompt}) {
		t.Fatalf("Stream = %q, %v", deltas, err)
	}
	for index := range 8 {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			t.Parallel()
			request := fmt.Sprintf("%s%d", prompt, index)
			text, err := runner.Run(t.Context(), request)
			if err != nil || text != "收到"+request {
				t.Fatalf("Run = %q, %v", text, err)
			}
		})
	}
}

func TestModelFailurePreservesOnlyEmittedText(t *testing.T) {
	modelFailure := errors.New("model unavailable")
	runner, err := blades.New("assistant", func(_ context.Context, _ string) iter.Seq2[string, error] {
		return func(yield func(string, error) bool) {
			if yield("partial", nil) {
				yield("", modelFailure)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	var deltas []string
	err = runner.Stream(t.Context(), "prompt", func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if !errors.Is(err, modelFailure) || !slices.Equal(deltas, []string{"partial"}) {
		t.Fatalf("Stream = %q, %v", deltas, err)
	}
	if text, err := runner.Run(t.Context(), "prompt"); text != "" || !errors.Is(err, modelFailure) {
		t.Fatalf("Run = %q, %v", text, err)
	}
}

func TestModelPanicStopsOnlyTheCurrentRun(t *testing.T) {
	upstreamFailure := errors.New("model refused the prompt")
	runner, err := blades.New("assistant", func(_ context.Context, prompt string) iter.Seq2[string, error] {
		if prompt == "factory" {
			panic("model factory failed")
		}
		return func(yield func(string, error) bool) {
			switch prompt {
			case "partial":
				if yield("partial text", nil) {
					panic(nil)
				}
			case "after failure":
				yield("", upstreamFailure)
				panic("model cleanup failed")
			default:
				yield(prompt, nil)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if text, err := runner.Run(ctx, "factory"); text != "" || !errors.Is(err, blades.ErrModelPanic) {
		t.Fatalf("Run after factory panic = %q, %v", text, err)
	}
	var fragments []string
	err = runner.Stream(ctx, "partial", func(fragment string) error {
		fragments = append(fragments, fragment)
		return nil
	})
	if !errors.Is(err, blades.ErrModelPanic) || !slices.Equal(fragments, []string{"partial text"}) {
		t.Fatalf("Stream after model panic = %q, %v", fragments, err)
	}
	if text, err := runner.Run(ctx, "after failure"); text != "" || !errors.Is(err, upstreamFailure) {
		t.Fatalf("Run after a reported model failure = %q, %v", text, err)
	}
	if text, err := runner.Run(ctx, "后续调用"); text != "后续调用" || err != nil {
		t.Fatalf("Run after recovery = %q, %v", text, err)
	}
}

func TestStreamCancellationStopsModel(t *testing.T) {
	for _, rejectDelta := range []bool{false, true} {
		name := "caller cancellation"
		if rejectDelta {
			name = "receiver failure"
		}
		t.Run(name, func(t *testing.T) {
			stopped := make(chan struct{})
			runner, err := blades.New("assistant", func(ctx context.Context, _ string) iter.Seq2[string, error] {
				return func(yield func(string, error) bool) {
					defer close(stopped)
					if yield("first", nil) {
						<-ctx.Done()
						yield("", ctx.Err())
					}
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := context.Canceled
			if rejectDelta {
				want = errors.New("downstream disconnected")
			}
			result := make(chan error, 1)
			go func() {
				result <- runner.Stream(ctx, "prompt", func(string) error {
					if rejectDelta {
						return want
					}
					cancel()
					return nil
				})
			}()
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("Stream = %v, want %v", err, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stream did not stop")
			}
			select {
			case <-stopped:
			default:
				t.Fatal("model was still running after stream returned")
			}
		})
	}
}

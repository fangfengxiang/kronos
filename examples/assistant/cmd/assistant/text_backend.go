package main

import (
	"context"
	"errors"
	"iter"

	"github.com/fangfengxiang/kronos/examples/textbackend"
)

var errTextBackendGeneration = errors.New("text backend generation failed")

type textBackend interface {
	Generate(context.Context, *textbackend.GenerateRequest) (*textbackend.GenerateResponse, error)
}

func modelForBackend(backend textBackend) func(context.Context, string) iter.Seq2[string, error] {
	return func(ctx context.Context, prompt string) iter.Seq2[string, error] {
		return func(yield func(string, error) bool) {
			response, err := backend.Generate(ctx, &textbackend.GenerateRequest{Prompt: prompt})
			if err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					err = errTextBackendGeneration
				}
				yield("", err)
				return
			}
			emitTextDeltas(response.GetText(), yield)
		}
	}
}

package blades

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"runtime/debug"

	"github.com/go-kratos/blades/content"
	"github.com/go-kratos/blades/model"
)

type textProvider struct {
	generate TextModel
}

func (textProvider) Name() string { return "text-model" }

func (provider textProvider) Generate(ctx context.Context, request *model.Request) (*model.Response, error) {
	return model.Collect(provider.Stream(ctx, request))
}

func (provider textProvider) Stream(ctx context.Context, request *model.Request) iter.Seq2[*model.Chunk, error] {
	return func(yield func(*model.Chunk, error) bool) {
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		if len(request.Messages) == 0 || request.Messages[len(request.Messages)-1].Role != model.RoleUser {
			yield(nil, errors.New("agent model requires a user prompt"))
			return
		}
		message := request.Messages[len(request.Messages)-1]
		prompt := content.TextFromParts(message.Parts)
		for delta, err := range provider.textStream(ctx, prompt) {
			if contextError := ctx.Err(); contextError != nil {
				yield(nil, contextError)
				return
			}
			if err != nil {
				yield(nil, err)
				return
			}
			if delta != "" && !yield(&model.Chunk{Parts: []content.Part{content.Text{Text: delta}}}, nil) {
				return
			}
		}
		if err := ctx.Err(); err != nil {
			yield(nil, err)
			return
		}
		yield(&model.Chunk{StopReason: model.StopEnd}, nil)
	}
}

func (provider textProvider) textStream(ctx context.Context, prompt string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		accepting, delivering := true, false
		defer func() {
			if value := recover(); value != nil {
				// A panic from the consumer belongs to its own execution boundary.
				if delivering {
					panic(value)
				}
				reportModelPanic(ctx, value)
				if accepting {
					yield("", ErrModelPanic)
				}
			}
		}()
		provider.generate(ctx, prompt)(func(delta string, err error) bool {
			if !accepting {
				return false
			}
			delivering = true
			accepting = yield(delta, err)
			delivering = false
			return accepting
		})
	}
}

func reportModelPanic(ctx context.Context, value any) {
	// Keep application log handlers from disrupting model error delivery.
	defer func() { _ = recover() }()
	slog.ErrorContext(ctx, "text model panicked", "panic_type", fmt.Sprintf("%T", value), "stack", string(debug.Stack()))
}

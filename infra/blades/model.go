package blades

import (
	"context"
	"errors"
	"iter"

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
		for delta, err := range provider.generate(ctx, prompt) {
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

package main

import (
	"context"
	"iter"
	"strings"

	"github.com/fangfengxiang/kronos/core/locale"
	"github.com/fangfengxiang/kronos/examples/assistant"
	"github.com/fangfengxiang/kronos/framework/localization"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

type textRunner interface {
	Run(ctx context.Context, prompt string) (string, error)
	Stream(ctx context.Context, prompt string, emit func(string) error) error
}

type agentService struct {
	runner   textRunner
	messages *locale.Catalog
}

func newAgentService(runner textRunner) (agentService, error) {
	invalidPrompt := assistant.ErrorReasonInvalidPrompt.String()
	messages, err := locale.NewCatalogFromBundles("en", map[string]map[string]string{
		"en":      {invalidPrompt: "prompt must not be blank"},
		"zh-Hans": {invalidPrompt: "提示词不能为空"},
	})
	if err != nil {
		return agentService{}, err
	}
	return agentService{runner: runner, messages: messages}, nil
}

func (service agentService) Run(ctx context.Context, request *assistant.RunRequest) (*assistant.RunResponse, error) {
	prompt := request.GetPrompt()
	if err := service.validatePrompt(ctx, prompt); err != nil {
		return nil, err
	}
	text, err := service.runner.Run(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &assistant.RunResponse{Text: text}, nil
}

func (service agentService) StreamRun(ctx context.Context, request *assistant.StreamRunRequest, emit func(string) error) error {
	prompt := request.GetPrompt()
	if err := service.validatePrompt(ctx, prompt); err != nil {
		return err
	}
	return service.runner.Stream(ctx, prompt, emit)
}

func (service agentService) validatePrompt(ctx context.Context, prompt string) error {
	if strings.TrimSpace(prompt) != "" {
		return nil
	}
	reason := assistant.ErrorReasonInvalidPrompt.String()
	tag, _ := localization.Negotiate(ctx, service.messages)
	message, _ := service.messages.Lookup(tag, reason)
	return &rpc.Error{Code: rpc.InvalidArgument, Reason: reason, Message: message.Text}
}

func echoText(ctx context.Context, prompt string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		if err := ctx.Err(); err != nil {
			yield("", err)
			return
		}
		emitTextDeltas(prompt, yield)
	}
}

func emitTextDeltas(text string, yield func(string, error) bool) {
	characters := []rune(text)
	firstDeltaLength := (len(characters) + 1) / 2
	if yield(string(characters[:firstDeltaLength]), nil) && firstDeltaLength < len(characters) {
		yield(string(characters[firstDeltaLength:]), nil)
	}
}

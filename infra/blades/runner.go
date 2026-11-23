// Package blades adapts the Blades runtime to Kronos text generation.
package blades

import (
	"context"
	"errors"
	"iter"
	"strings"

	upstream "github.com/go-kratos/blades"
	"github.com/go-kratos/blades/event"
)

// ErrIncomplete indicates that a run ended without a successful terminal event.
var ErrIncomplete = errors.New("agent run ended without completion")

// TextModel generates deltas for a single prompt. Its iterator must honor
// context cancellation and stop when yield returns false.
type TextModel func(ctx context.Context, prompt string) iter.Seq2[string, error]

// Runner executes independent single-prompt text runs. The model supplied by
// the caller determines whether concurrent calls are supported.
type Runner struct {
	engine *upstream.Runner
}

// New constructs a text runner around a model supplied by the application.
func New(name string, generate TextModel) (*Runner, error) {
	if strings.TrimSpace(name) == "" || generate == nil {
		return nil, errors.New("agent runner requires a name and text model")
	}
	agent, err := upstream.NewAgent(name, upstream.WithModel(textProvider{generate: generate}))
	if err != nil {
		return nil, err
	}
	return &Runner{engine: upstream.NewRunner(agent)}, nil
}

// Run returns the entire output of a successful run.
func (runner *Runner) Run(ctx context.Context, prompt string) (string, error) {
	var text strings.Builder
	if err := runner.Stream(ctx, prompt, func(delta string) error {
		text.WriteString(delta)
		return nil
	}); err != nil {
		return "", err
	}
	return text.String(), nil
}

// Stream delivers ordered text fragments and returns after the runtime stops.
// An error from emit cancels the model and is returned to the caller.
func (runner *Runner) Stream(ctx context.Context, prompt string, emit func(string) error) error {
	if emit == nil {
		return errors.New("agent stream requires a text receiver")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	outputs, err := runner.engine.RunStream(runContext, event.NewPrompt(prompt))
	if err != nil {
		return err
	}
	var completedTurn, done bool
	var runError error
	for output := range outputs {
		// Blades v0.6 sends final events without selecting on cancellation.
		// Drain the channel after a failure so its output goroutine can exit.
		if runError != nil {
			continue
		}
		if runError = runContext.Err(); runError != nil {
			continue
		}
		switch output := output.(type) {
		case event.TextDelta:
			runError = emit(output.Text)
		case event.TurnEnd:
			runError = output.Err
			completedTurn = output.StopReason == event.StopEnd
		case event.Error:
			runError = output.Err
		case event.Done:
			done = true
		}
		if runError != nil {
			cancel()
		}
	}
	if runError != nil {
		return runError
	}
	if err := runContext.Err(); err != nil {
		return err
	}
	if !completedTurn || !done {
		return ErrIncomplete
	}
	return nil
}

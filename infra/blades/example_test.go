package blades_test

import (
	"context"
	"fmt"
	"iter"
	"time"

	"github.com/fangfengxiang/kronos/infra/blades"
)

func ExampleRunner_Run() {
	model := func(ctx context.Context, prompt string) iter.Seq2[string, error] {
		return func(yield func(string, error) bool) {
			if ctx.Err() == nil && yield("received: ", nil) && ctx.Err() == nil {
				yield(prompt, nil)
			}
		}
	}
	runner, err := blades.New("assistant", model)
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	text, err := runner.Run(ctx, "hello")
	if err != nil {
		panic(err)
	}
	fmt.Println(text)
	// Output: received: hello
}

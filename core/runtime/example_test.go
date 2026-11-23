package runtime_test

import (
	"fmt"

	kruntime "github.com/fangfengxiang/kronos/core/runtime"
)

func ExampleGo() {
	completed := make(chan kruntime.Result, 1)
	kruntime.Go(func() { panic("background job failed") },
		kruntime.WithLogger(func(info kruntime.PanicInfo) {
			fmt.Printf("panic type: %T\n", info.Value)
		}),
		kruntime.WithObserver(func(result kruntime.Result) {
			completed <- result
		}),
	)
	fmt.Println("panicked:", (<-completed).Outcome == kruntime.Panicked)
	// Output:
	// panic type: string
	// panicked: true
}

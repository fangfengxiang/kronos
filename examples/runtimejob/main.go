package main

import (
	"fmt"
	"log"

	kruntime "github.com/fangfengxiang/kronos/core/runtime"
)

func main() {
	documents := []string{"Kronos", "你好"}
	var totalBytes int
	completed := make(chan kruntime.Result, 1)
	kruntime.Go(func() {
		for _, document := range documents {
			totalBytes += len(document)
		}
	}, kruntime.WithObserver(func(result kruntime.Result) {
		completed <- result
	}))
	if result := <-completed; result.Outcome != kruntime.Returned {
		log.Fatal("document summary failed")
	}
	fmt.Printf("documents=%d bytes=%d\n", len(documents), totalBytes)
}

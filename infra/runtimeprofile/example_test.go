package runtimeprofile_test

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/fangfengxiang/kronos/infra/runtimeprofile"
)

func ExampleNewServer() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	server, err := runtimeprofile.NewServer(listener)
	if err != nil {
		panic(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Start(context.Background()) }()
	client := http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/debug/pprof/goroutine")
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	profile, err := gzip.NewReader(response.Body)
	if err != nil {
		panic(err)
	}
	defer profile.Close()
	profileBytes, err := io.Copy(io.Discard, profile)
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Stop(ctx); err != nil {
		panic(err)
	}
	if err := <-served; err != nil {
		panic(err)
	}
	fmt.Println(response.StatusCode, profileBytes > 0)
	// Output: 200 true
}

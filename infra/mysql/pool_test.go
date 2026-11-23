package mysql_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/mysql"
)

func TestOpenRejectsInvalidConfiguration(t *testing.T) {
	const secret = "test-password-only"
	for _, config := range []mysql.Config{
		{},
		{DSN: "user:" + secret + "@tcp(127.0.0.1:3306)/test?parseTime=invalid"},
		{DSN: "user:" + secret + "@tcp(127.0.0.1:3306)/test", MaxIdleConnections: -1},
	} {
		pool, err := mysql.Open(t.Context(), config)
		if pool != nil || !errors.Is(err, mysql.ErrInvalidConfig) {
			t.Fatalf("invalid configuration returned %v, %v", pool, err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatal("error exposed part of the DSN")
		}
	}
}

func TestOpenCancellationClosesPendingHandshake(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		pool, err := mysql.Open(ctx, mysql.Config{DSN: "user@tcp(" + listener.Addr().String() + ")/test"})
		if pool != nil {
			pool.Close()
			t.Error("Open returned a pool before the server completed a handshake")
		}
		result <- err
	}()
	connection, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceling Open returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Open did not return after cancellation")
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("connection was not closed, read returned %v", err)
	}
}

func ExampleOpen_invalidSettings() {
	_, err := mysql.Open(context.Background(), mysql.Config{})
	fmt.Println(errors.Is(err, mysql.ErrInvalidConfig))
	// Output: true
}

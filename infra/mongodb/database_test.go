package mongodb_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/mongodb"
)

func TestOpenRejectsInvalidConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, config := range []mongodb.Config{
		{},
		{URI: "mongodb://127.0.0.1:27017"},
		{URI: "https://user:fixture-password@example.com", Database: "profiles"},
		{URI: "mongodb://127.0.0.1:27017/?w=0", Database: "profiles"},
	} {
		database, err := mongodb.Open(ctx, config)
		if database != nil || err != mongodb.ErrInvalidConfig {
			t.Fatalf("invalid configuration produced %v, %v", database, err)
		}
	}
}

func TestOpenCancellationClosesMonitoredConnection(t *testing.T) {
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
		database, err := mongodb.Open(ctx, mongodb.Config{URI: "mongodb://" + listener.Addr().String(), Database: "profiles"})
		if database != nil {
			_ = database.Close(context.Background())
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
			t.Fatalf("canceled bootstrap = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("database bootstrap did not return after cancellation")
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, connection); err != nil {
		t.Fatalf("monitor connection did not close: %v", err)
	}
}

func ExampleOpen_invalidConfiguration() {
	_, err := mongodb.Open(context.Background(), mongodb.Config{})
	fmt.Println(errors.Is(err, mongodb.ErrInvalidConfig))
	// Output: true
}

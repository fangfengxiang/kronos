package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/kafka"
)

func TestRejectInvalidKafkaConfiguration(t *testing.T) {
	tests := []kafka.Config{
		{},
		{Brokers: []string{"localhost"}, Topic: "events"},
		{Brokers: []string{"localhost:0"}, Topic: "events"},
		{Brokers: []string{"localhost:9092"}, Topic: "invalid/topic"},
		{Brokers: []string{"localhost:9092"}, Topic: "events", SASL: &kafka.PlainAuth{Username: "test"}},
	}
	for _, config := range tests {
		if _, err := kafka.OpenPublisher(t.Context(), config); err != kafka.ErrInvalidConfig {
			t.Fatalf("invalid configuration returned %v", err)
		}
		if _, err := kafka.NewConsumer(config, "probe"); err != kafka.ErrInvalidConfig {
			t.Fatalf("invalid configuration returned %v", err)
		}
	}
}

func TestCancellationReleasesNegotiatingConnection(t *testing.T) {
	t.Run("publisher startup", func(t *testing.T) {
		assertBrokerCancellation(t, func(ctx context.Context, config kafka.Config) error {
			publisher, err := kafka.OpenPublisher(ctx, config)
			if publisher != nil {
				publisher.Close()
			}
			return err
		})
	})
	t.Run("consumer group", func(t *testing.T) {
		assertBrokerCancellation(t, func(ctx context.Context, config kafka.Config) error {
			consumer, err := kafka.NewConsumer(config, "probe")
			if err != nil {
				return err
			}
			return consumer.Run(ctx, func(context.Context, kafka.Message) error {
				return errors.New("unexpected record")
			})
		})
	})
}

func assertBrokerCancellation(t *testing.T, run func(context.Context, kafka.Config) error) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opened := make(chan error, 1)
	go func() {
		opened <- run(ctx, kafka.Config{Brokers: []string{listener.Addr().String()}, Topic: "events"})
	}()
	connection, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	cancel()
	select {
	case err := <-opened:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Kafka negotiation returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Kafka negotiation did not stop after cancellation")
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, connection); err != nil {
		t.Fatalf("Kafka client did not release its connection: %v", err)
	}
}

func ExampleOpenPublisher_invalidConfiguration() {
	_, err := kafka.OpenPublisher(context.Background(), kafka.Config{})
	fmt.Println(errors.Is(err, kafka.ErrInvalidConfig))
	// Output: true
}

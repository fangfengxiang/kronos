package kafka

import (
	"bytes"
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Publisher owns a Kafka client for one topic and supports concurrent Publish calls.
type Publisher struct {
	client       *kgo.Client
	cancelClient context.CancelFunc
}

// OpenPublisher establishes a Kafka connection within ctx and transfers ownership
// of the client to the caller.
func OpenPublisher(ctx context.Context, config Config) (*Publisher, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	options, err := clientOptions(config)
	if err != nil {
		return nil, err
	}
	clientContext, cancelClient := context.WithCancel(context.Background())
	client, err := kgo.NewClient(append(options,
		kgo.WithContext(clientContext), kgo.DefaultProduceTopic(config.Topic), kgo.RequiredAcks(kgo.AllISRAcks()),
	)...)
	if err != nil {
		cancelClient()
		return nil, ErrInvalidConfig
	}
	// Kafka protocol negotiation reads until the client context ends. Forward
	// startup cancellation only until the connection changes ownership.
	stopStartupCancellation := context.AfterFunc(ctx, cancelClient)
	pingError := client.Ping(ctx)
	stopStartupCancellation()
	if err := ctx.Err(); err != nil {
		pingError = err
	}
	if pingError != nil {
		cancelClient()
		client.Close()
		return nil, operationError(ctx, pingError)
	}
	return &Publisher{client: client, cancelClient: cancelClient}, nil
}

// Publish returns success once Kafka acknowledges the record. Cancellation
// stops the wait, so Kafka may still accept a record that has already been sent.
func (publisher *Publisher) Publish(ctx context.Context, key, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// An in-flight idempotent send can retain its record after ctx is canceled.
	record := &kgo.Record{Key: bytes.Clone(key), Value: bytes.Clone(value)}
	acknowledgment := make(chan error, 1)
	publisher.client.Produce(ctx, record, func(_ *kgo.Record, err error) {
		acknowledgment <- err
	})
	select {
	case err := <-acknowledgment:
		return operationError(ctx, err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close releases the client. Callers finish their Publish calls before shutdown.
func (publisher *Publisher) Close() {
	publisher.client.Close()
	publisher.cancelClient()
}

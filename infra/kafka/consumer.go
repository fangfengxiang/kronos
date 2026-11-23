package kafka

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Message identifies a consumed Kafka record. Key and Value may be nil.
type Message struct {
	Key       []byte
	Value     []byte
	Partition int32
	Offset    int64
}

// Consumer holds settings for one topic and group. Each Run opens a client.
type Consumer struct {
	options []kgo.Opt
}

// NewConsumer prepares consumption from the committed group offset, or the
// earliest available record when the group has no offset.
func NewConsumer(config Config, group string) (*Consumer, error) {
	if strings.TrimSpace(group) == "" {
		return nil, ErrInvalidConfig
	}
	options, err := clientOptions(config)
	if err != nil {
		return nil, err
	}
	options = append(options,
		kgo.ConsumeTopics(config.Topic), kgo.ConsumerGroup(group), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(),
	)
	return &Consumer{options: options}, nil
}

// Run calls handle sequentially and commits each successful record. It returns
// when ctx ends, a fetch or commit fails, or handle returns an error. Handlers
// can receive a record again after an interrupted or unsuccessful commit.
func (consumer *Consumer) Run(ctx context.Context, handle func(context.Context, Message) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if handle == nil {
		return ErrInvalidConfig
	}
	clientContext, cancelClient := context.WithCancel(context.Background())
	options := append([]kgo.Opt{kgo.WithContext(clientContext)}, consumer.options...)
	client, err := kgo.NewClient(options...)
	if err != nil {
		cancelClient()
		return ErrInvalidConfig
	}
	defer closeGroup(client, cancelClient)
	for {
		if err := consumeRecord(ctx, client, handle); err != nil {
			return err
		}
	}
}

func consumeRecord(ctx context.Context, client *kgo.Client, handle func(context.Context, Message) error) error {
	fetched := client.PollRecords(ctx, 1)
	defer client.AllowRebalance()
	if err := fetched.Err(); err != nil {
		return operationError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	records := fetched.RecordIter()
	if records.Done() {
		return nil
	}
	record := records.Next()
	if err := handle(ctx, Message{Key: record.Key, Value: record.Value, Partition: record.Partition, Offset: record.Offset}); err != nil {
		return fmt.Errorf("handle Kafka record: %w", err)
	}
	return operationError(ctx, client.CommitRecords(ctx, record))
}

func closeGroup(client *kgo.Client, cancelClient context.CancelFunc) {
	client.AllowRebalance()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = client.LeaveGroupContext(ctx)
	cancelClient()
	client.Close()
}

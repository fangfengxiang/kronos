package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/examples/textbackend/internal/eventconfig"
	"github.com/fangfengxiang/kronos/framework/log"
	"github.com/fangfengxiang/kronos/infra/kafka"
	"google.golang.org/protobuf/proto"
)

func main() {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stopSignals()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.NewJSON(os.Stderr, nil).ErrorContext(ctx, "text backend events failed", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("textbackend-events", flag.ContinueOnError)
	kafkaFlags := eventconfig.RegisterKafkaFlags(flags)
	group := flags.String("kafka-group", "", "Kafka consumer group for generated-text events")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	config, err := kafkaFlags.Config()
	if err != nil {
		return err
	}
	consumer, err := kafka.NewConsumer(config, *group)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	return consumer.Run(ctx, func(_ context.Context, record kafka.Message) error {
		var event textbackend.GenerationRecorded
		if err := proto.Unmarshal(record.Value, &event); err != nil || event.GetRequestId() == "" || string(record.Key) != event.GetRequestId() {
			return errors.New("invalid text generation event")
		}
		return encoder.Encode(generationOutput{
			RequestID: event.GetRequestId(), OutputUTF8Bytes: event.GetOutputUtf8Bytes(),
			Partition: record.Partition, Offset: record.Offset,
		})
	})
}

type generationOutput struct {
	RequestID       string `json:"request_id"`
	OutputUTF8Bytes uint64 `json:"output_utf8_bytes"`
	Partition       int32  `json:"partition"`
	Offset          int64  `json:"offset"`
}

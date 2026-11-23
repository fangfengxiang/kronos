package main

import (
	"context"
	"fmt"
	"time"

	"github.com/fangfengxiang/kronos/examples/textbackend"
	"github.com/fangfengxiang/kronos/framework/request"
	"github.com/fangfengxiang/kronos/infra/kafka"
	"google.golang.org/protobuf/proto"
)

type kafkaGenerationRecorder struct {
	publisher *kafka.Publisher
}

func (recorder kafkaGenerationRecorder) RecordGeneration(ctx context.Context, outputBytes uint64) error {
	requestID := request.ID(ctx)
	payload, err := proto.Marshal(&textbackend.GenerationRecorded{RequestId: requestID, OutputUtf8Bytes: outputBytes})
	if err != nil {
		return fmt.Errorf("encode generation event: %w", err)
	}
	publishContext, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err := recorder.publisher.Publish(publishContext, []byte(requestID), payload); err != nil {
		if contextError := ctx.Err(); contextError != nil {
			return contextError
		}
		return fmt.Errorf("record generation event: %w", kafka.ErrUnavailable)
	}
	return nil
}

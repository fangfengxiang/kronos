package binding

import (
	"context"
	"errors"
	"fmt"
	"io"

	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"github.com/fangfengxiang/kronos/framework/rpc"
)

type streamRunFunc func(context.Context, *wire.StreamRunRequest, func(*wire.StreamRunResponse) error) error

func newStreamRunner(client wire.AssistantClient) streamRunFunc {
	return func(ctx context.Context, request *wire.StreamRunRequest, receive func(*wire.StreamRunResponse) error) error {
		streamContext, cancel := context.WithCancel(withOutgoingStreamRequest(ctx))
		defer cancel()
		stream, err := client.StreamRun(streamContext, request)
		if err != nil {
			return fromWireError(err)
		}
		var completed *wire.StreamRunResponse
		for {
			event, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				if completed == nil {
					return fmt.Errorf("%w: missing completed event", rpc.ErrInvalidStream)
				}
				return receive(completed)
			}
			if err != nil {
				return fromWireError(err)
			}
			if completed != nil {
				return fmt.Errorf("%w: event after completion", rpc.ErrInvalidStream)
			}
			switch event.GetEvent().(type) {
			case *wire.StreamRunResponse_TextDelta:
				if err := receive(event); err != nil {
					return err
				}
			case *wire.StreamRunResponse_Completed:
				completed = event
			default:
				return fmt.Errorf("%w: unrecognized stream event", rpc.ErrInvalidStream)
			}
		}
	}
}

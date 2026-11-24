package assistant

import (
	"context"

	"github.com/fangfengxiang/kronos/examples/assistant/internal/binding"
	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/platform/kronos"
)

// BindService binds both Assistant methods to a unified Kronos application.
func BindService(unary Service, streams StreamService) kronos.Service {
	descriptor := (*RunRequest)(nil).ProtoReflect().Descriptor().ParentFile().Services().ByName("Assistant")
	var run func(context.Context, *RunRequest) (*RunResponse, error)
	if unary != nil {
		guarded := rpc.RecoverUnary(unary.Run)
		run = func(ctx context.Context, request *RunRequest) (*RunResponse, error) {
			ctx, cancel := context.WithTimeout(ctx, binding.UnaryTimeout)
			defer cancel()
			response, err := guarded(ctx, request)
			return response, binding.PublicError(err)
		}
	}
	var stream func(context.Context, *StreamRunRequest, func(*StreamRunResponse) error) error
	if streams != nil {
		guarded := rpc.RecoverStream(streams.StreamRun)
		stream = func(ctx context.Context, request *StreamRunRequest, emit func(*StreamRunResponse) error) error {
			return binding.PublicError(binding.StreamEvents(ctx, request, guarded, emit))
		}
	}
	return kronos.Bind(descriptor, kronos.Unary("Run", (*RunRequest)(nil), (*RunResponse)(nil), run)).
		WithServerStreams(kronos.ServerStream("StreamRun", (*StreamRunRequest)(nil), (*StreamRunResponse)(nil), stream))
}

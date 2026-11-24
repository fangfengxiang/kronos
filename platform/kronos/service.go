package kronos

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/fangfengxiang/kronos/framework/rpc"
	"github.com/fangfengxiang/kronos/platform/kronos/internal/transport"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Error is the transport-neutral error returned by a business handler.
type Error = rpc.Error

const (
	InvalidArgument = rpc.InvalidArgument
	Internal        = rpc.Internal
	Unavailable     = rpc.Unavailable
)

// Service describes the bound methods of a Protobuf service.
type Service struct {
	descriptor protoreflect.ServiceDescriptor
	methods    []UnaryMethod
	streams    []ServerStreamMethod
}

func rpcMethodPath(service, method string) string {
	return "/" + service + "/" + method
}

// UnaryMethod holds a typed handler and its Protobuf message types.
type UnaryMethod struct {
	methodTypes
	invoke func(context.Context, proto.Message) (proto.Message, error)
}

// Unary makes a bound handler for the named Protobuf method.
func Unary[Request, Response proto.Message](
	name protoreflect.Name, input Request, output Response,
	handler func(context.Context, Request) (Response, error),
) UnaryMethod {
	method := UnaryMethod{methodTypes: methodTypes{name, input, output}}
	if handler != nil {
		guarded := rpc.RecoverCall(handler)
		method.invoke = func(ctx context.Context, message proto.Message) (proto.Message, error) {
			return guarded(ctx, message.(Request))
		}
	}
	return method
}

// Bind creates a service definition. Register validates its complete contract.
func Bind(descriptor protoreflect.ServiceDescriptor, methods ...UnaryMethod) Service {
	return Service{descriptor: descriptor, methods: slices.Clone(methods)}
}

type methodTypes struct {
	name          protoreflect.Name
	input, output proto.Message
}

// ServerStreamMethod holds a typed server-streaming handler.
type ServerStreamMethod struct {
	methodTypes
	invoke func(context.Context, proto.Message, func(proto.Message) error) error
}

// ServerStream binds a single request to ordered response messages. The handler
// calls send sequentially while it is active and returns when sending fails.
func ServerStream[Request, Event proto.Message](name protoreflect.Name, input Request, event Event,
	handler func(context.Context, Request, func(Event) error) error) ServerStreamMethod {
	method := ServerStreamMethod{methodTypes: methodTypes{name, input, event}}
	if handler != nil {
		guarded := rpc.RecoverStream(func(ctx context.Context, input *Request, send func(Event) error) error {
			return handler(ctx, *input, send)
		})
		method.invoke = func(ctx context.Context, message proto.Message, send func(proto.Message) error) error {
			request := message.(Request)
			var sendError error
			err := guarded(ctx, &request, func(event Event) error {
				if sendError == nil {
					switch {
					case ctx.Err() != nil:
						sendError = ctx.Err()
					case proto.Message(event) == nil || !event.ProtoReflect().IsValid():
						sendError = errors.New("invalid server stream event")
					default:
						sendError = send(event)
					}
				}
				return sendError
			})
			if canceled := ctx.Err(); canceled != nil {
				return canceled
			}
			if sendError != nil {
				return sendError
			}
			return err
		}
	}
	return method
}

// WithServerStreams adds streaming methods to the service before registration.
func (service Service) WithServerStreams(methods ...ServerStreamMethod) Service {
	service.streams = append(slices.Clone(service.streams), methods...)
	return service
}

func (service Service) prepare() (transport.Service, error) {
	if service.descriptor == nil {
		return transport.Service{}, errors.New("a Protobuf service descriptor is required")
	}
	descriptors := service.descriptor.Methods()
	count := len(service.methods) + len(service.streams)
	if count == 0 || count != descriptors.Len() {
		return transport.Service{}, fmt.Errorf("%s requires a binding for each method", service.descriptor.FullName())
	}
	result := transport.Service{
		Name: string(service.descriptor.FullName()), ShortName: string(service.descriptor.Name()),
		Filename: service.descriptor.ParentFile().Path(),
		Methods:  make([]transport.Method, 0, count),
	}
	registered := make(map[protoreflect.Name]struct{}, count)
	addMethod := func(types methodTypes, binding transport.Method, streaming bool) error {
		descriptor := descriptors.ByName(types.name)
		if descriptor == nil {
			return fmt.Errorf("%s has no method named %q", result.Name, types.name)
		}
		if descriptor.IsStreamingClient() || descriptor.IsStreamingServer() != streaming {
			return fmt.Errorf("%s has an incompatible stream shape", descriptor.FullName())
		}
		if _, duplicate := registered[types.name]; duplicate {
			return fmt.Errorf("%s is bound more than once", descriptor.FullName())
		}
		if binding.Invoke == nil && binding.Stream == nil || proto.MessageName(types.input) != descriptor.Input().FullName() ||
			proto.MessageName(types.output) != descriptor.Output().FullName() {
			return fmt.Errorf("%s has an invalid handler or Protobuf message type", descriptor.FullName())
		}
		registered[types.name] = struct{}{}
		inputType := types.input.ProtoReflect().Type()
		binding.Name, binding.Descriptor = string(types.name), descriptor
		binding.NewRequest = func() proto.Message { return inputType.New().Interface() }
		result.Methods = append(result.Methods, binding)
		return nil
	}
	for _, method := range service.methods {
		if err := addMethod(method.methodTypes, transport.Method{Invoke: method.invoke}, false); err != nil {
			return transport.Service{}, err
		}
	}
	for _, method := range service.streams {
		if err := addMethod(method.methodTypes, transport.Method{Stream: method.invoke}, true); err != nil {
			return transport.Service{}, err
		}
	}
	return result, nil
}

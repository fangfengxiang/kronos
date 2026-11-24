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
}

func rpcMethodPath(service, method string) string {
	return "/" + service + "/" + method
}

// UnaryMethod holds a typed handler and its Protobuf message types.
type UnaryMethod struct {
	name   protoreflect.Name
	input  proto.Message
	output proto.Message
	invoke func(context.Context, proto.Message) (proto.Message, error)
}

// Unary makes a bound handler for the named Protobuf method.
func Unary[Request, Response proto.Message](
	name protoreflect.Name, input Request, output Response,
	handler func(context.Context, Request) (Response, error),
) UnaryMethod {
	method := UnaryMethod{name: name, input: input, output: output}
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

func (service Service) prepare() (transport.Service, error) {
	if service.descriptor == nil {
		return transport.Service{}, errors.New("a Protobuf service descriptor is required")
	}
	descriptors := service.descriptor.Methods()
	if len(service.methods) == 0 || len(service.methods) != descriptors.Len() {
		return transport.Service{}, fmt.Errorf("%s requires a binding for each method", service.descriptor.FullName())
	}
	result := transport.Service{
		Name: string(service.descriptor.FullName()), ShortName: string(service.descriptor.Name()),
		Filename: service.descriptor.ParentFile().Path(),
		Methods:  make([]transport.Method, 0, len(service.methods)),
	}
	registered := make(map[protoreflect.Name]struct{}, len(service.methods))
	for _, method := range service.methods {
		descriptor := descriptors.ByName(method.name)
		if descriptor == nil {
			return transport.Service{}, fmt.Errorf("%s has no method named %q", result.Name, method.name)
		}
		if descriptor.IsStreamingClient() || descriptor.IsStreamingServer() {
			return transport.Service{}, fmt.Errorf("%s requires a unary method", descriptor.FullName())
		}
		if _, duplicate := registered[method.name]; duplicate {
			return transport.Service{}, fmt.Errorf("%s is bound more than once", descriptor.FullName())
		}
		if method.invoke == nil || proto.MessageName(method.input) != descriptor.Input().FullName() ||
			proto.MessageName(method.output) != descriptor.Output().FullName() {
			return transport.Service{}, fmt.Errorf("%s has an invalid handler or Protobuf message type", descriptor.FullName())
		}
		registered[method.name] = struct{}{}
		inputType := method.input.ProtoReflect().Type()
		result.Methods = append(result.Methods, transport.Method{
			Name: string(method.name), Descriptor: descriptor,
			NewRequest: func() proto.Message { return inputType.New().Interface() }, Invoke: method.invoke,
		})
	}
	return result, nil
}

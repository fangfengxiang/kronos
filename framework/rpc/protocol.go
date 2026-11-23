package rpc

import (
	"errors"
	"fmt"
)

// Protocol identifies the wire protocol an RPC client uses.
type Protocol string

const (
	ProtocolGRPC    Protocol = "grpc"
	ProtocolMicroV2 Protocol = "micro-v2"
)

// ErrUnsupportedProtocol identifies an unknown client protocol.
var ErrUnsupportedProtocol = errors.New("unsupported RPC protocol")

type clientOptions struct {
	protocol Protocol
}

// ClientOption configures a client at construction time.
type ClientOption func(*clientOptions)

// WithProtocol selects a wire protocol for the client.
func WithProtocol(protocol Protocol) ClientOption {
	return func(options *clientOptions) { options.protocol = protocol }
}

// ResolveProtocol applies options for a client binding. The default is gRPC.
// If multiple options specify a protocol, the last one takes effect.
func ResolveProtocol(options ...ClientOption) (Protocol, error) {
	config := clientOptions{protocol: ProtocolGRPC}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	switch config.protocol {
	case ProtocolGRPC, ProtocolMicroV2:
		return config.protocol, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedProtocol, config.protocol)
	}
}

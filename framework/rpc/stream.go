package rpc

import "errors"

// ErrStreamingUnsupported indicates that the selected client transport does
// not support the requested stream operation.
var ErrStreamingUnsupported = errors.New("rpc streaming is unavailable for this client")

// ErrInvalidStream indicates that the peer ended or sent events in a way that
// violates the service's stream protocol.
var ErrInvalidStream = errors.New("invalid rpc event stream")

package rpc

import "context"

// Server is the portion of a transport owned by application assembly.
type Server interface {
	Start(context.Context) error
	Stop(context.Context) error
}

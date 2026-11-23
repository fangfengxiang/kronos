// Package request carries request metadata across application calls.
package request

import (
	"context"
	"crypto/rand"
)

// IDHeader is the shared HTTP and gRPC key for a request correlation ID.
const IDHeader = "x-request-id"

const maxIDBytes = 128

type idKey struct{}

// WithID sets the correlation ID for downstream calls. An empty or invalid ID
// clears the ID for the returned context.
func WithID(ctx context.Context, id string) context.Context {
	if !validID(id) {
		id = ""
	}
	return context.WithValue(ctx, idKey{}, id)
}

// WithIncomingID establishes the ID for a request. A missing, repeated, or
// invalid header value is replaced with a new ID.
func WithIncomingID(ctx context.Context, values ...string) context.Context {
	if len(values) == 1 && validID(values[0]) {
		return context.WithValue(ctx, idKey{}, values[0])
	}
	return context.WithValue(ctx, idKey{}, rand.Text())
}

// ID returns the correlation ID, or an empty string when it has not been set.
func ID(ctx context.Context) string {
	id, _ := ctx.Value(idKey{}).(string)
	return id
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > maxIDBytes {
		return false
	}
	for index := range len(id) {
		char := id[index]
		switch {
		case 'a' <= char && char <= 'z', 'A' <= char && char <= 'Z', '0' <= char && char <= '9':
		case char == '.', char == '_', char == '-':
		default:
			return false
		}
	}
	return true
}

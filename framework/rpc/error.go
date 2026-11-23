package rpc

// Code identifies a failure class independently of its transport representation.
type Code string

const (
	InvalidArgument Code = "INVALID_ARGUMENT"
	Internal        Code = "INTERNAL"
	Unavailable     Code = "UNAVAILABLE"
)

// ServiceUnavailableReason identifies a request rejected because an instance
// is currently unable to serve new work.
const ServiceUnavailableReason = "SERVICE_UNAVAILABLE"

// Error is a public RPC failure. Reason identifies the service failure and
// Message is safe to present to the caller.
type Error struct {
	Code    Code
	Reason  string
	Message string
}

func (failure *Error) Error() string {
	return failure.Message
}

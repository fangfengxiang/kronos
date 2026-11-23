// Package microv2 adapts the go-micro v2 unary Protobuf protocol over HTTP.
package microv2

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"mime"
	"net/http"
	"strconv"
	"time"
)

const (
	protobufContentType = "application/protobuf"
	framedContentType   = "application/proto-rpc"
	maxRequestBytes     = 1 << 20
	maxRequestTimeout   = 30 * time.Second
)

// Unary receives and returns the Protobuf payload for an endpoint.
type Unary func(context.Context, []byte) ([]byte, error)

// Error is a failure safe to return to a go-micro client.
type Error struct {
	Code    int
	Reason  string
	Message string
}

func (failure *Error) Error() string { return failure.Message }

type wireError struct {
	ID     string `json:"id"`
	Code   int    `json:"code"`
	Detail string `json:"detail"`
	Status string `json:"status"`
}

// NewHandler builds a handler for one service and its named unary endpoints.
func NewHandler(service string, endpoints map[string]Unary) http.Handler {
	return handler{service: service, endpoints: maps.Clone(endpoints)}
}

type handler struct {
	service   string
	endpoints map[string]Unary
}

func (target handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(response, request)
		return
	}
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	contentType := protobufContentType
	if header := request.Header.Get("Content-Type"); header != "" {
		var err error
		contentType, _, err = mime.ParseMediaType(header)
		if err != nil || contentType != protobufContentType && contentType != framedContentType {
			http.Error(response, "unsupported RPC content type", http.StatusUnsupportedMediaType)
			return
		}
	}
	if request.Header.Get("Micro-Stream") != "" || request.Header.Get("Micro-Topic") != "" {
		http.Error(response, "unary RPC required", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxRequestBytes))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(response, "request is too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(response, "invalid RPC body", http.StatusBadRequest)
		}
		return
	}

	endpoint := request.Header.Get("Micro-Endpoint")
	if endpoint == "" {
		endpoint = request.Header.Get("Micro-Method")
	}
	var frame envelope
	if contentType == framedContentType {
		var valid bool
		frame, body, valid = readFrames(body)
		if !valid {
			http.Error(response, "invalid RPC frames", http.StatusBadRequest)
			return
		}
		if endpoint == "" {
			endpoint = frame.method
		}
	}

	var payload []byte
	var failure *Error
	if invoke := target.endpoints[endpoint]; invoke != nil && request.Header.Get("Micro-Service") == target.service {
		payload, failure = call(request, body, invoke)
	} else {
		failure = &Error{Code: http.StatusNotFound, Reason: "METHOD_NOT_FOUND", Message: "RPC endpoint is not registered"}
	}
	writeResponse(response, request, contentType, frame, payload, failure)
}

func call(request *http.Request, body []byte, invoke Unary) ([]byte, *Error) {
	timeout := maxRequestTimeout
	if header := request.Header.Get("Timeout"); header != "" {
		nanoseconds, err := strconv.ParseInt(header, 10, 64)
		if err != nil || nanoseconds < 0 {
			return nil, &Error{Code: http.StatusBadRequest, Reason: "INVALID_TIMEOUT", Message: "invalid RPC timeout"}
		}
		if nanoseconds > 0 {
			timeout = min(timeout, time.Duration(nanoseconds))
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()
	payload, err := invoke(ctx, body)
	if err == nil {
		return payload, nil
	}
	var failure *Error
	switch {
	case errors.As(err, &failure) && failure != nil:
		return nil, failure
	case errors.Is(err, context.DeadlineExceeded):
		return nil, &Error{Code: http.StatusGatewayTimeout, Reason: "DEADLINE_EXCEEDED", Message: "RPC deadline exceeded"}
	case errors.Is(err, context.Canceled):
		return nil, &Error{Code: 499, Reason: "CANCELED", Message: "RPC canceled"}
	default:
		return nil, &Error{Code: http.StatusInternalServerError, Reason: "INTERNAL", Message: "RPC failed"}
	}
}

func writeResponse(response http.ResponseWriter, request *http.Request, contentType string, frame envelope, payload []byte, failure *Error) {
	header := response.Header()
	header.Set("Content-Type", contentType)
	for _, key := range []string{"Micro-Id", "Micro-Service", "Micro-Endpoint", "Micro-Method"} {
		if value := request.Header.Get(key); value != "" {
			header.Set(key, value)
		}
	}
	errorJSON := marshalWireError(failure)
	if contentType == framedContentType {
		payload = encodeFrames(frame, payload, errorJSON, maxResponseBytes)
		if payload == nil {
			errorJSON = marshalWireError(&Error{
				Code: http.StatusInternalServerError, Reason: "RESPONSE_TOO_LARGE", Message: "RPC response exceeds size limit",
			})
			// Keep the sequence for correlation and omit the original method.
			payload = encodeFrames(envelope{sequence: frame.sequence}, nil, errorJSON, maxResponseBytes)
		}
	}
	if errorJSON != nil {
		header.Set("Micro-Error", string(errorJSON))
	}
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(payload)
}

func marshalWireError(failure *Error) []byte {
	if failure == nil {
		return nil
	}
	data, _ := json.Marshal(wireError{
		ID: failure.Reason, Code: failure.Code, Detail: failure.Message, Status: http.StatusText(failure.Code),
	})
	return data
}

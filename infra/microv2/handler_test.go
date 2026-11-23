package microv2

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestUnaryEncodings(t *testing.T) {
	const endpoint = "Assistant.Run"
	handler := NewHandler("assistant", map[string]Unary{endpoint: func(_ context.Context, payload []byte) ([]byte, error) {
		if string(payload) == "fail" {
			return nil, &Error{Code: http.StatusBadRequest, Reason: "INVALID_PROMPT", Message: "empty prompt"}
		}
		return payload, nil
	}})
	for _, contentType := range []string{protobufContentType, framedContentType} {
		t.Run(contentType, func(t *testing.T) {
			for _, input := range []string{"hello", "fail"} {
				payload := []byte(input)
				frame := envelope{method: endpoint, sequence: 37}
				if contentType == framedContentType {
					payload = encodeFrames(frame, payload, nil, maxRequestBytes)
				}
				request := rpcRequest(contentType, endpoint, payload)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusOK || response.Header().Get("Micro-Id") != "37" {
					t.Fatalf("unexpected RPC response: %d %v", response.Code, response.Header())
				}
				failure := response.Header().Get("Micro-Error")
				var want []byte
				if input == "fail" {
					if !strings.Contains(failure, `"id":"INVALID_PROMPT"`) || !strings.Contains(failure, `"code":400`) {
						t.Fatalf("unexpected public error: %s", failure)
					}
				} else {
					if failure != "" {
						t.Fatalf("successful call returned error: %s", failure)
					}
					want = []byte(input)
				}
				if contentType == framedContentType {
					var errorBody []byte
					if failure != "" {
						errorBody = []byte(failure)
					}
					want = encodeFrames(frame, want, errorBody, maxResponseBytes)
				}
				if !bytes.Equal(response.Body.Bytes(), want) {
					t.Fatalf("response = %x, want %x", response.Body.Bytes(), want)
				}
			}
		})
	}
}

func TestBoundsAndUnknownMethod(t *testing.T) {
	handler := NewHandler("assistant", nil)
	for _, test := range []struct {
		name     string
		encoding string
		payload  []byte
		status   int
	}{
		{"false frame length", framedContentType, []byte{255, 255, 255, 255}, http.StatusBadRequest},
		{"oversized body", protobufContentType, make([]byte, maxRequestBytes+1), http.StatusRequestEntityTooLarge},
		{"unknown endpoint", protobufContentType, nil, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, rpcRequest(test.encoding, "Assistant.Unknown", test.payload))
			if response.Code != test.status {
				t.Fatalf("response status = %d, want %d", response.Code, test.status)
			}
			if test.status == http.StatusOK && !strings.Contains(response.Header().Get("Micro-Error"), "METHOD_NOT_FOUND") {
				t.Errorf("unexpected error: %v", response.Header())
			}
		})
	}
}

func TestFramedResponseLimit(t *testing.T) {
	const endpoint = "Assistant.Run"
	requestFrame := envelope{method: endpoint, sequence: 37}
	requestBody := encodeFrames(requestFrame, nil, nil, maxRequestBytes)
	payloadAtLimit := maxResponseBytes - len(requestBody)
	for _, test := range []struct {
		name        string
		payloadSize int
		failure     error
		wantError   bool
	}{
		{name: "at limit", payloadSize: payloadAtLimit},
		{name: "one byte over", payloadSize: payloadAtLimit + 1, wantError: true},
		{name: "oversized error", failure: &Error{Code: http.StatusBadRequest, Reason: "FIXTURE_ERROR", Message: strings.Repeat("x", maxResponseBytes)}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var payload []byte
			if test.payloadSize != 0 {
				tag := protowire.AppendTag(nil, 1, protowire.BytesType)
				payload = protowire.AppendBytes(tag, make([]byte, test.payloadSize-4))
				if len(payload) != test.payloadSize {
					t.Fatalf("invalid fixture size %d, want %d", len(payload), test.payloadSize)
				}
			}
			handler := NewHandler("assistant", map[string]Unary{endpoint: func(context.Context, []byte) ([]byte, error) {
				return payload, test.failure
			}})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, rpcRequest(framedContentType, endpoint, requestBody))
			if response.Code != http.StatusOK || response.Body.Len() > maxResponseBytes {
				t.Fatalf("HTTP status %d, framed body size %d", response.Code, response.Body.Len())
			}
			message := response.Result()
			defer message.Body.Close()
			got, err := decodeResponse(message, requestFrame.sequence)
			if !test.wantError {
				if err != nil || !bytes.Equal(got, payload) || response.Body.Len() != maxResponseBytes {
					t.Fatalf("boundary response: payload size %d, framed size %d, error %v", len(got), response.Body.Len(), err)
				}
				return
			}
			var failure *Error
			if got != nil || !errors.As(err, &failure) || failure.Code != http.StatusInternalServerError || failure.Reason != "RESPONSE_TOO_LARGE" {
				t.Fatalf("oversized response: payload size %d, error %v", len(got), err)
			}
			frame, _, valid := readFrames(response.Body.Bytes())
			if !valid || frame.failure == "" || frame.failure != message.Header.Get("Micro-Error") {
				t.Fatal("response frame and Micro-Error must carry the same size error")
			}
		})
	}
}

func TestClientTimeoutReachesHandler(t *testing.T) {
	const endpoint = "Assistant.Run"
	handler := NewHandler("assistant", map[string]Unary{endpoint: func(ctx context.Context, _ []byte) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	request := rpcRequest(protobufContentType, endpoint, nil)
	request.Header.Set("Timeout", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if !strings.Contains(response.Header().Get("Micro-Error"), `"code":504`) {
		t.Fatalf("unexpected timeout response: %v", response.Header())
	}
}

func rpcRequest(contentType, endpoint string, payload []byte) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Micro-Service", "assistant")
	request.Header.Set("Micro-Endpoint", endpoint)
	request.Header.Set("Micro-Id", "37")
	return request
}

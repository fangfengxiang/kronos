package microv2

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
					payload = encodeFrames(frame, payload, nil)
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
					want = encodeFrames(frame, want, errorBody)
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

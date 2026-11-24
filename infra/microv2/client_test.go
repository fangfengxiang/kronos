package microv2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClientSendsExactRequestIDHeader(t *testing.T) {
	const endpoint = "Assistant.Run"
	received := make(chan []string, 2)
	legacy := NewHandler("assistant", map[string]Unary{
		endpoint: func(_ context.Context, payload []byte) ([]byte, error) { return payload, nil },
	})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		received <- request.Header.Values("X-Request-ID")
		legacy.ServeHTTP(response, request)
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := client.CallWithRequestID(ctx, "assistant", endpoint, nil, "request-01", RequestMetadata{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Call(ctx, "assistant", endpoint, nil); err != nil {
		t.Fatal(err)
	}
	if first, second := <-received, <-received; !slices.Equal(first, []string{"request-01"}) || len(second) != 0 {
		t.Fatalf("request ID headers = %v and %v", first, second)
	}
}

func TestClientSendsLegacyEnvelopeAndDeadline(t *testing.T) {
	const endpoint = "Assistant.Run"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/" || request.Header.Get("Content-Type") != framedContentType ||
			request.Header.Get("Micro-Service") != "assistant" || request.Header.Get("Micro-Endpoint") != endpoint || request.Header.Get("Micro-Method") != endpoint {
			t.Errorf("unexpected request: %s %s %v", request.Method, request.URL, request.Header)
		}
		timeout, err := strconv.ParseInt(request.Header.Get("Timeout"), 10, 64)
		if err != nil || timeout <= 0 || timeout > int64(3*time.Second) {
			t.Errorf("timeout header = %d ns, %v", timeout, err)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		frame, payload, valid := readFrames(body)
		if !valid || frame.method != endpoint || strconv.FormatUint(frame.sequence, 10) != request.Header.Get("Micro-Id") {
			t.Errorf("unexpected request frames: %v %x", frame, body)
		}
		response.Header().Set("Content-Type", framedContentType)
		_, _ = response.Write(encodeFrames(frame, payload, nil, maxResponseBytes))
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	payload := []byte("payload")
	got, err := client.Call(ctx, "assistant", endpoint, payload)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("Call = %q, %v", got, err)
	}
}

func TestClientRejectsInvalidResponse(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
		want string
	}{
		{"mismatched sequence", encodeFrames(envelope{sequence: 99}, nil, nil, maxResponseBytes), "invalid sequence"},
		{"malformed frame", []byte{255, 255, 255, 255}, "invalid frames"},
		{"oversized response", make([]byte, maxResponseBytes+1), "response exceeds size limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {framedContentType}}, Body: io.NopCloser(bytes.NewReader(test.body))}
			if _, err := decodeResponse(response, 1); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decodeResponse = %v, want %q", err, test.want)
			}
		})
	}
}

func TestClientMatchesOfficialLegacyErrorByHeader(t *testing.T) {
	body := encodeFrames(envelope{}, nil, []byte(`{"id":"INVALID_PROMPT","code":400,"detail":"empty prompt"}`), maxResponseBytes)
	for _, responseID := range []string{"12", "13"} {
		response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {framedContentType}}, Body: io.NopCloser(bytes.NewReader(body))}
		response.Header.Set("Micro-Id", responseID)
		_, err := decodeResponse(response, 12)
		var failure *Error
		if responseID == "12" && (!errors.As(err, &failure) || failure.Code != 400 || failure.Reason != "INVALID_PROMPT") {
			t.Fatalf("matching response error = %v", err)
		}
		if responseID == "13" && (err == nil || !strings.Contains(err.Error(), "invalid sequence")) {
			t.Fatalf("mismatched response error = %v", err)
		}
	}
}

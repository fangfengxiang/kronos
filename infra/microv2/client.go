package microv2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Client sends unary proto-rpc requests to a direct go-micro v2 endpoint.
// A Client can be used concurrently.
type Client struct {
	address   string
	transport *http.Client
	sequence  atomic.Uint64
}

// NewClient accepts host:port or an HTTP(S) origin.
func NewClient(address string) (*Client, error) {
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	origin, err := url.Parse(address)
	if err != nil || origin.Scheme != "http" && origin.Scheme != "https" || origin.Hostname() == "" ||
		origin.User != nil || origin.Path != "" && origin.Path != "/" || origin.ForceQuery || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("go-micro v2 requires an HTTP(S) origin")
	}
	origin.Path = "/"
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	return &Client{address: origin.String(), transport: &http.Client{
		Transport:     &http.Transport{Protocols: protocols, IdleConnTimeout: 90 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Call sends a Protobuf payload and returns the decoded response payload.
func (client *Client) Call(ctx context.Context, service, endpoint string, payload []byte) ([]byte, error) {
	return client.CallWithTraceContext(ctx, service, endpoint, payload, "", "")
}

// CallWithTraceContext sends a unary request with optional W3C trace headers.
func (client *Client) CallWithTraceContext(ctx context.Context, service, endpoint string, payload []byte, traceparent, tracestate string) ([]byte, error) {
	sequence := client.sequence.Add(1)
	frame := encodeFrames(envelope{method: endpoint, sequence: sequence}, payload, nil, maxRequestBytes)
	if frame == nil {
		return nil, errors.New("go-micro v2 request exceeds size limit")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.address, bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", framedContentType)
	request.Header.Set("Accept", framedContentType)
	request.Header.Set("Micro-Id", strconv.FormatUint(sequence, 10))
	request.Header.Set("Micro-Service", service)
	request.Header.Set("Micro-Method", endpoint)
	request.Header.Set("Micro-Endpoint", endpoint)
	if traceparent != "" {
		request.Header.Set("traceparent", traceparent)
		if tracestate != "" {
			request.Header.Set("tracestate", tracestate)
		}
	}
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		request.Header.Set("Timeout", strconv.FormatInt(int64(remaining), 10))
	}
	response, err := client.transport.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return decodeResponse(response, sequence)
}

func decodeResponse(response *http.Response, sequence uint64) ([]byte, error) {
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("go-micro v2 response HTTP status: %d", response.StatusCode)
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != framedContentType {
		return nil, errors.New("go-micro v2 response has an invalid content type")
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxResponseBytes {
		return nil, errors.New("go-micro v2 response exceeds size limit")
	}
	header, decoded, valid := readFrames(encoded)
	if !valid {
		return nil, errors.New("go-micro v2 response has invalid frames")
	}
	failure := header.failure
	if failure == "" {
		failure = response.Header.Get("Micro-Error")
	}
	// The official v2 server omits the frame sequence on handler errors but
	// echoes the request ID in the HTTP header.
	matchesLegacyErrorID := failure != "" && header.sequence == 0 && response.Header.Get("Micro-Id") == strconv.FormatUint(sequence, 10)
	if header.sequence != sequence && !matchesLegacyErrorID {
		return nil, errors.New("go-micro v2 response has invalid sequence")
	}
	if failure == "" {
		return decoded, nil
	}
	var remoteError wireError
	if err := json.Unmarshal([]byte(failure), &remoteError); err != nil || remoteError.Code < 400 || remoteError.Code > 599 {
		return nil, errors.New("go-micro v2 response has an invalid RPC error")
	}
	return nil, &Error{Code: remoteError.Code, Reason: remoteError.ID, Message: remoteError.Detail}
}

// CloseIdleConnections releases pooled connections; in-flight calls can finish.
func (client *Client) CloseIdleConnections() {
	client.transport.CloseIdleConnections()
}

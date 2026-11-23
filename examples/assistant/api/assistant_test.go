package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	assistantv1 "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
)

func TestAssistantMethods(t *testing.T) {
	service := assistantv1.File_kronos_assistant_v1_assistant_proto.Services().ByName("Assistant")
	if service == nil || service.FullName() != "kronos.assistant.v1.Assistant" {
		t.Fatalf("unexpected Assistant service: %v", service)
	}

	run := service.Methods().ByName("Run")
	if run == nil || run.IsStreamingClient() || run.IsStreamingServer() {
		t.Fatalf("Run must be unary: %v", run)
	}
	httpRule, ok := proto.GetExtension(run.Options(), annotations.E_Http).(*annotations.HttpRule)
	if !ok || httpRule.GetPost() != "/v1/assistant:run" || httpRule.GetBody() != "*" {
		t.Fatalf("unexpected Run HTTP mapping: %v", httpRule)
	}
	if len(httpRule.GetAdditionalBindings()) != 0 {
		t.Fatalf("unexpected Run HTTP bindings: %v", httpRule.GetAdditionalBindings())
	}

	streamRun := service.Methods().ByName("StreamRun")
	if streamRun == nil || streamRun.IsStreamingClient() || !streamRun.IsStreamingServer() {
		t.Fatalf("StreamRun must be server-streaming: %v", streamRun)
	}
	streamRule, ok := proto.GetExtension(streamRun.Options(), annotations.E_Http).(*annotations.HttpRule)
	if !ok || streamRule.GetPost() != "/v1/assistant:streamRun" || streamRule.GetBody() != "*" {
		t.Fatalf("unexpected StreamRun SSE mapping: %v", streamRule)
	}
}

func TestStreamRunEventRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		event *assistantv1.StreamRunResponse
	}{
		{
			name: "text delta",
			event: &assistantv1.StreamRunResponse{Event: &assistantv1.StreamRunResponse_TextDelta{
				TextDelta: "你好",
			}},
		},
		{
			name: "completion",
			event: &assistantv1.StreamRunResponse{Event: &assistantv1.StreamRunResponse_Completed{
				Completed: &assistantv1.RunCompleted{},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encodedEvent, err := proto.Marshal(test.event)
			if err != nil {
				t.Fatal(err)
			}
			var decodedEvent assistantv1.StreamRunResponse
			if err := proto.Unmarshal(encodedEvent, &decodedEvent); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(&decodedEvent, test.event) {
				t.Fatalf("decoded event = %v, want %v", &decodedEvent, test.event)
			}
		})
	}
}

func TestAssistantErrorReasons(t *testing.T) {
	tests := []struct {
		reason assistantv1.ErrorReason
		name   string
		number int32
	}{
		{assistantv1.ErrorReason_ERROR_REASON_UNSPECIFIED, "ERROR_REASON_UNSPECIFIED", 0},
		{assistantv1.ErrorReason_ERROR_REASON_INVALID_PROMPT, "ERROR_REASON_INVALID_PROMPT", 1},
		{assistantv1.ErrorReason_ERROR_REASON_RUN_FAILED, "ERROR_REASON_RUN_FAILED", 2},
	}

	for _, test := range tests {
		if test.reason.String() != test.name || int32(test.reason) != test.number {
			t.Errorf("error reason = (%s, %d), want (%s, %d)", test.reason, test.reason, test.name, test.number)
		}
	}
}

type echoAssistant struct {
	assistantv1.UnimplementedAssistantServer
}

func (echoAssistant) Run(_ context.Context, request *assistantv1.RunRequest) (*assistantv1.RunResponse, error) {
	return &assistantv1.RunResponse{Text: request.GetPrompt()}, nil
}

func TestGeneratedAssistantHTTPBinding(t *testing.T) {
	server := kratoshttp.NewServer()
	assistantv1.RegisterAssistantHTTPServer(server, echoAssistant{})

	var routes []kratoshttp.RouteInfo
	if err := server.WalkRoute(func(route kratoshttp.RouteInfo) error {
		routes = append(routes, route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Method != http.MethodPost || routes[0].Path != "/v1/assistant:run" ||
		routes[1].Method != http.MethodPost || routes[1].Path != "/v1/assistant:streamRun" {
		t.Fatalf("registered routes = %+v, want Run and StreamRun POST", routes)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/assistant:run?prompt=query", strings.NewReader(`{"prompt":"你好"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, response = %s", recorder.Code, recorder.Body)
	}
	var response assistantv1.RunResponse
	if err := protojson.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.GetText() != "你好" {
		t.Errorf("text = %q, want prompt from the JSON body", response.GetText())
	}
}

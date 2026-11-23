package assistant_test

import (
	"testing"

	"github.com/fangfengxiang/kronos/examples/assistant"
	wire "github.com/fangfengxiang/kronos/examples/assistant/internal/wire/kronos/assistant/v1"
	"google.golang.org/protobuf/proto"
)

var _ assistant.AssistantUnaryService = (*assistantService)(nil)

func TestPublicContractMatchesStandardProtobuf(t *testing.T) {
	public := &assistant.StreamRunResponse{
		Event: &assistant.StreamRunResponse_TextDelta{TextDelta: "你好"},
	}
	native := &wire.StreamRunResponse{
		Event: &wire.StreamRunResponse_TextDelta{TextDelta: "你好"},
	}
	if !proto.Equal(public, native) || proto.MessageName(public) != proto.MessageName(native) {
		t.Fatalf("public stream event %v differs from the standard generated message %v", public, native)
	}
	encoded, err := proto.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &wire.StreamRunResponse{}
	if err := proto.Unmarshal(encoded, decoded); err != nil || !proto.Equal(decoded, native) {
		t.Fatalf("standard Protobuf decoded (%v, %v)", decoded, err)
	}
	completed, err := proto.Marshal(&wire.StreamRunResponse{
		Event: &wire.StreamRunResponse_Completed{Completed: &wire.RunCompleted{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(completed, public); err != nil || public.GetCompleted() == nil {
		t.Fatalf("public completed event = (%v, %v)", public, err)
	}
	if assistant.ErrorReasonInvalidPrompt != wire.ErrorReason_ERROR_REASON_INVALID_PROMPT ||
		assistant.ErrorReason_ERROR_REASON_RUN_FAILED != wire.ErrorReason_ERROR_REASON_RUN_FAILED {
		t.Fatal("public error enums differ from the standard generated values")
	}
}

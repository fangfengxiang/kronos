package request_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/framework/request"
)

func TestRequestIDContext(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	first := request.WithID(parent, "client_01.abc-test")
	second := request.WithID(first, "second")
	cleared := request.WithID(second, "bad id")
	if request.ID(parent) != "" || request.ID(first) != "client_01.abc-test" || request.ID(second) != "second" || request.ID(cleared) != "" {
		t.Fatal("setting or clearing the ID changed the wrong context")
	}
	wantDeadline, _ := parent.Deadline()
	if deadline, ok := cleared.Deadline(); !ok || !deadline.Equal(wantDeadline) {
		t.Fatalf("request ID context deadline = %v, want %v", deadline, wantDeadline)
	}
	if id := strings.Repeat("A", 128); request.ID(request.WithIncomingID(parent, id)) != id {
		t.Fatal("a valid ID at the length limit was not preserved")
	}
}

func TestInvalidIncomingRequestIDGetsReplaced(t *testing.T) {
	testCases := []struct {
		name   string
		values []string
	}{
		{name: "missing"},
		{name: "empty", values: []string{""}},
		{name: "repeated", values: []string{"same", "same"}},
		{name: "combined", values: []string{"first,second"}},
		{name: "whitespace", values: []string{" leading"}},
		{name: "non ASCII", values: []string{"关联"}},
		{name: "oversized", values: []string{strings.Repeat("a", 129)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			first := request.ID(request.WithIncomingID(t.Context(), testCase.values...))
			second := request.ID(request.WithIncomingID(t.Context(), testCase.values...))
			if first == "" || second == "" || first == second || request.ID(request.WithIncomingID(t.Context(), first)) != first {
				t.Fatalf("invalid input yielded IDs %q and %q", first, second)
			}
		})
	}
}

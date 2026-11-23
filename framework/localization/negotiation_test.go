package localization_test

import (
	"slices"
	"testing"

	"github.com/fangfengxiang/kronos/framework/localization"
)

func TestOrderedPreferences(t *testing.T) {
	ctx := localization.WithAcceptLanguage(t.Context(),
		"en;q=0.5, zh-CN;q=0.9, fr;q=0, de;q=invalid, *",
		"ja;q=0.9, it;q=2, es;q=NaN, ko;q=0.3")
	matcher := &recordingMatcher{}
	tag, matched := localization.Negotiate(ctx, matcher)
	if !matched || tag != "zh-CN" {
		t.Fatalf("Negotiate = %q, %v", tag, matched)
	}
	if want := []string{"zh-CN", "ja", "en", "ko"}; !slices.Equal(matcher.preferences, want) {
		t.Errorf("matcher preferences = %v, want %v", matcher.preferences, want)
	}
}

func TestFallbackAndSnapshot(t *testing.T) {
	values := []string{"ja;q=0", "*", "en;q=broken"}
	ctx := localization.WithAcceptLanguage(t.Context(), values...)
	values[0] = "zh-CN"
	copyOfValues := localization.AcceptLanguage(ctx)
	copyOfValues[1] = "zh-CN"
	matcher := &recordingMatcher{}
	if tag, matched := localization.Negotiate(ctx, matcher); tag != "en" || matched || len(matcher.preferences) != 0 {
		t.Fatalf("fallback = %q, %v from %v", tag, matched, matcher.preferences)
	}
	if got := localization.AcceptLanguage(t.Context()); got != nil {
		t.Errorf("absent preferences = %v", got)
	}
}

func TestIncomingAndOutgoingPreferences(t *testing.T) {
	incomingContext := localization.WithIncomingAcceptLanguage(t.Context(), "zh-CN")
	if got := localization.AcceptLanguage(incomingContext); got != nil {
		t.Fatalf("outgoing preferences on an incoming context = %v", got)
	}
	callContext := localization.WithAcceptLanguage(incomingContext, "en")
	if got := localization.AcceptLanguage(callContext); !slices.Equal(got, []string{"en"}) {
		t.Fatalf("outgoing preferences = %v", got)
	}
	if tag, matched := localization.Negotiate(callContext, &recordingMatcher{}); tag != "zh-CN" || !matched {
		t.Fatalf("incoming negotiation = %q, %v", tag, matched)
	}
}

type recordingMatcher struct {
	preferences []string
}

func (matcher *recordingMatcher) Match(preferences ...string) (string, bool) {
	matcher.preferences = slices.Clone(preferences)
	if len(preferences) > 0 {
		return preferences[0], true
	}
	return "en", false
}

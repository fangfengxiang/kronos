package localization_test

import (
	"context"
	"fmt"
	"slices"

	"github.com/fangfengxiang/kronos/framework/localization"
)

func ExampleNegotiate() {
	ctx := localization.WithIncomingAcceptLanguage(context.Background(), "fr, zh-CN;q=0.9, en;q=0.5")
	tag, matched := localization.Negotiate(ctx, supportedLanguages{"en", "zh-CN"})
	fmt.Println(tag, matched)
	// Output: zh-CN true
}

type supportedLanguages []string

func (supported supportedLanguages) Match(preferences ...string) (string, bool) {
	for _, preferred := range preferences {
		if slices.Contains(supported, preferred) {
			return preferred, true
		}
	}
	return "en", false
}

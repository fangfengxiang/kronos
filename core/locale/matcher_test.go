package locale_test

import (
	"testing"

	"github.com/fangfengxiang/kronos/core/locale"
)

func TestNewMatcherRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		fallback  string
		supported []string
	}{
		{"invalid default", "??", []string{"en"}},
		{"empty supported set", "en", nil},
		{"default not registered", "en", []string{"zh-Hans"}},
		{"duplicate canonical tags", "en-US", []string{"EN-us", "en-US"}},
		{"duplicate suppressed script", "en-US", []string{"en-US", "en-Latn-US"}},
		{"extension on resource tag", "en", []string{"en", "en-u-ca-japanese"}},
		{"invalid supported tag", "en", []string{"en", "en-!!!"}},
		{"undefined language", "en", []string{"en", "und"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if matcher, err := locale.NewMatcher(tt.fallback, tt.supported...); matcher != nil || err == nil {
				t.Fatalf("NewMatcher = (%v, %v), want configuration error", matcher, err)
			}
		})
	}
}

func TestMatcherSelectsRegisteredLanguages(t *testing.T) {
	m, err := locale.NewMatcher("EN-gb", "zh-Hans", "zh-Hant", "en-GB")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		prefs   []string
		tag     string
		matched bool
	}{
		{"no preference", nil, "en-GB", false},
		{"unsupported", []string{"fr"}, "en-GB", false},
		{"explicit default matched", []string{"en-GB"}, "en-GB", true},
		{"case normalization", []string{" EN-gb "}, "en-GB", true},
		{"simplified by region", []string{"zh-CN"}, "zh-Hans", true},
		{"traditional by region", []string{"zh-TW"}, "zh-Hant", true},
		{"registered resource tag", []string{"en-AU-u-ca-japanese"}, "en-GB", true},
		{"malformed then supported", []string{"en-!!!", "zh-TW"}, "zh-Hant", true},
		{"unsupported then supported", []string{"de", "zh-CN"}, "zh-Hans", true},
		{"earlier regional preference beats later exact", []string{"en-AU", "zh-Hant"}, "en-GB", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tag, matched := m.Match(tt.prefs...); tag != tt.tag || matched != tt.matched {
				t.Errorf("Match(%v) = (%q, %t), want (%q, %t)", tt.prefs, tag, matched, tt.tag, tt.matched)
			}
		})
	}
}

func TestMatcherSkipsLowConfidenceScriptMatch(t *testing.T) {
	m, err := locale.NewMatcher("en", "zh-Hans", "en")
	if err != nil {
		t.Fatal(err)
	}
	if tag, matched := m.Match("zh-TW"); tag != "en" || matched {
		t.Errorf("Match(zh-TW) = (%q, %t), want default en", tag, matched)
	}
	if tag, matched := m.Match("zh-TW", "en"); tag != "en" || !matched {
		t.Errorf("Match(zh-TW, en) = (%q, %t), want en matched by second preference", tag, matched)
	}
}

func BenchmarkMatcherMatch(b *testing.B) {
	m, err := locale.NewMatcher("en", "zh-Hans", "zh-Hant", "en")
	if err != nil {
		b.Fatal(err)
	}
	for _, tt := range []struct{ name, tag string }{
		{"registered", "en"},
		{"regional", "zh-TW"},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.Match(tt.tag)
			}
		})
	}
}

package locale_test

import (
	"fmt"
	"testing"

	"github.com/fangfengxiang/kronos/core/locale"
)

func TestNewCatalogRejectsInvalidConfiguration(t *testing.T) {
	m, err := locale.NewMatcher("en", "en", "zh-Hant")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		matcher *locale.Matcher
		bundles map[string]map[string]string
	}{
		{"nil matcher", nil, nil},
		{"zero matcher", new(locale.Matcher), nil},
		{"missing default bundle", m, map[string]map[string]string{"zh-Hant": {}}},
		{"unsupported language", m, map[string]map[string]string{"en": {}, "fr": {}}},
		{"invalid tag", m, map[string]map[string]string{"en": {}, "!": {}}},
		{"duplicate canonical bundles", m, map[string]map[string]string{"en": {}, "EN": {}}},
		{"duplicate suppressed script", m, map[string]map[string]string{"en": {}, "en-Latn": {}}},
		{"extension on resource tag", m, map[string]map[string]string{"en": {}, "en-u-ca-japanese": {}}},
		{"empty message key", m, map[string]map[string]string{"en": {"": "missing key"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if catalog, err := locale.NewCatalog(tt.matcher, tt.bundles); catalog != nil || err == nil {
				t.Fatalf("NewCatalog = (%v, %v), want configuration error", catalog, err)
			}
		})
	}
}

func TestCatalogLookupSnapshotsDataAndReportsResourceLanguage(t *testing.T) {
	m, err := locale.NewMatcher("en", "en", "zh-Hant", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	bundles := map[string]map[string]string{
		"EN":      {"welcome": "Hello", "retry": "Try again"},
		"zh-hant": {"welcome": "您好", "quiet": ""},
	}
	catalog, err := locale.NewCatalog(m, bundles)
	if err != nil {
		t.Fatal(err)
	}
	bundles["zh-hant"]["welcome"] = "changed"
	delete(bundles["EN"], "retry")
	delete(bundles, "zh-hant")

	tests := []struct {
		name string
		tag  string
		key  string
		want locale.Message
		ok   bool
	}{
		{"selected translation", "zh-Hant", "welcome", locale.Message{Text: "您好", Language: "zh-Hant"}, true},
		{"missing translation", "zh-Hant", "retry", locale.Message{Text: "Try again", Language: "en"}, true},
		{"untranslated language", "zh-Hans", "retry", locale.Message{Text: "Try again", Language: "en"}, true},
		{"explicit empty", "zh-Hant", "quiet", locale.Message{Text: "", Language: "zh-Hant"}, true},
		{"key missing everywhere", "zh-Hant", "unknown", locale.Message{}, false},
		{"key missing from default", "en", "unknown", locale.Message{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := catalog.Lookup(tt.tag, tt.key); got != tt.want || ok != tt.ok {
				t.Errorf("Lookup(%q, %q) = (%+v, %t), want (%+v, %t)", tt.tag, tt.key, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func ExampleCatalog_Lookup() {
	m, err := locale.NewMatcher("en", "zh-Hant", "en")
	if err != nil {
		panic(err)
	}
	catalog, err := locale.NewCatalog(m, map[string]map[string]string{
		"zh-Hant": {"welcome": "您好"},
		"en":      {"welcome": "Hello", "retry": "Try again"},
	})
	if err != nil {
		panic(err)
	}
	tag, matched := m.Match("zh-TW")
	welcome, _ := catalog.Lookup(tag, "welcome")
	retry, _ := catalog.Lookup(tag, "retry")
	fmt.Println(tag, matched)
	fmt.Println(welcome.Language, welcome.Text)
	fmt.Println(retry.Language, retry.Text)
	// Output:
	// zh-Hant true
	// zh-Hant 您好
	// en Try again
}

func BenchmarkCatalogLookup(b *testing.B) {
	m, err := locale.NewMatcher("en", "zh-Hant", "en")
	if err != nil {
		b.Fatal(err)
	}
	catalog, err := locale.NewCatalog(m, map[string]map[string]string{
		"zh-Hant": {"welcome": "您好"},
		"en":      {"retry": "Try again"},
	})
	if err != nil {
		b.Fatal(err)
	}
	for _, tt := range []struct{ name, key string }{
		{"selected", "welcome"},
		{"fallback", "retry"},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				catalog.Lookup("zh-Hant", tt.key)
			}
		})
	}
}

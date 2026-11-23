// Package locale selects supported language tags and resolves localized messages.
package locale

import (
	"fmt"
	"strings"

	"golang.org/x/text/language"
)

// Matcher selects registered language tags. It is safe for concurrent use
// after construction with NewMatcher.
type Matcher struct {
	defaultTag string
	tags       []string
	registered map[string]struct{}
	matcher    language.Matcher
}

// NewMatcher constructs a matcher for the supported language tags. defaultTag
// must be included in supported. Equivalent canonical tags may be listed once.
// Registered tags cannot contain extensions.
func NewMatcher(defaultTag string, supported ...string) (*Matcher, error) {
	fallback, err := parseResourceTag(defaultTag)
	if err != nil {
		return nil, fmt.Errorf("invalid default language: %w", err)
	}
	tags := []language.Tag{fallback}
	names := []string{fallback.String()}
	registered := make(map[string]struct{}, len(supported))
	for _, raw := range supported {
		tag, err := parseResourceTag(raw)
		if err != nil {
			return nil, err
		}
		name := tag.String()
		if _, exists := registered[name]; exists {
			return nil, fmt.Errorf("duplicate supported language %q", name)
		}
		registered[name] = struct{}{}
		if name != names[0] {
			tags = append(tags, tag)
			names = append(names, name)
		}
	}
	if _, exists := registered[names[0]]; !exists {
		return nil, fmt.Errorf("default language %q is not supported", names[0])
	}
	return &Matcher{
		defaultTag: names[0],
		tags:       names,
		registered: registered,
		matcher:    language.NewMatcher(tags, language.PreferSameScript(false)),
	}, nil
}

// Match checks preferences in order. It returns a registered language tag and
// whether a preference matched with high confidence or better. If no preference
// matches, it returns the configured default and false.
func (m *Matcher) Match(preferences ...string) (tag string, matched bool) {
	for _, preferred := range preferences {
		preferred = strings.TrimSpace(preferred)
		if _, exists := m.registered[preferred]; exists {
			return preferred, true
		}
		tag, err := parseTag(preferred)
		if err != nil {
			continue
		}
		_, index, confidence := m.matcher.Match(tag)
		if confidence >= language.High {
			return m.tags[index], true
		}
	}
	return m.defaultTag, false
}

func parseTag(raw string) (language.Tag, error) {
	tag, err := language.BCP47.Parse(strings.TrimSpace(raw))
	if err != nil {
		return language.Und, fmt.Errorf("invalid language tag %q: %w", raw, err)
	}
	base, _, _ := tag.Raw()
	if base.String() == "und" {
		return language.Und, fmt.Errorf("language tag %q does not name a language", raw)
	}
	return tag, nil
}

func parseResourceTag(raw string) (language.Tag, error) {
	tag, err := parseTag(raw)
	if err != nil {
		return language.Und, err
	}
	if len(tag.Extensions()) != 0 {
		return language.Und, fmt.Errorf("resource language %q contains an extension", raw)
	}
	return tag, nil
}

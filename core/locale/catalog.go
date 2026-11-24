package locale

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Message contains the text and the registered language of the resource used.
type Message struct {
	Text     string
	Language string
}

// Catalog is a read-only snapshot of localized text keyed by message ID.
// A constructed catalog is safe for concurrent use.
type Catalog struct {
	defaultTag string
	bundles    map[string]map[string]string
	matcher    *Matcher
}

// NewCatalog copies bundles from the resource owner. Each bundle language must
// be registered in m, including a bundle for the default language.
func NewCatalog(m *Matcher, bundles map[string]map[string]string) (*Catalog, error) {
	if m == nil || m.defaultTag == "" {
		return nil, errors.New("locale catalog requires a configured matcher")
	}
	snapshot := make(map[string]map[string]string, len(bundles))
	for raw, messages := range bundles {
		tag, err := parseResourceTag(raw)
		if err != nil {
			return nil, err
		}
		name := tag.String()
		if _, exists := m.registered[name]; !exists {
			return nil, fmt.Errorf("catalog language %q is not supported", name)
		}
		if _, exists := snapshot[name]; exists {
			return nil, fmt.Errorf("duplicate catalog language %q", name)
		}
		if _, exists := messages[""]; exists {
			return nil, fmt.Errorf("catalog language %q contains an empty message key", name)
		}
		snapshot[name] = maps.Clone(messages)
	}
	if _, exists := snapshot[m.defaultTag]; !exists {
		return nil, fmt.Errorf("missing bundle for default language %q", m.defaultTag)
	}
	return &Catalog{defaultTag: m.defaultTag, bundles: snapshot, matcher: m}, nil
}

// NewCatalogFromBundles constructs a catalog whose supported languages are
// the bundle keys. The default language must have a bundle.
func NewCatalogFromBundles(defaultTag string, bundles map[string]map[string]string) (*Catalog, error) {
	matcher, err := NewMatcher(defaultTag, slices.Sorted(maps.Keys(bundles))...)
	if err != nil {
		return nil, err
	}
	return NewCatalog(matcher, bundles)
}

// Match selects a language from the ordered preferences using the catalog's
// matcher. It reports whether a preference matched.
func (c *Catalog) Match(preferences ...string) (string, bool) {
	return c.matcher.Match(preferences...)
}

// Lookup checks the selected registered tag, then the default bundle. A
// configured empty string is a valid message. A missing key returns false.
func (c *Catalog) Lookup(tag, key string) (Message, bool) {
	if message, exists := c.bundles[tag][key]; exists {
		return Message{Text: message, Language: tag}, true
	}
	if tag != c.defaultTag {
		if message, exists := c.bundles[c.defaultTag][key]; exists {
			return Message{Text: message, Language: c.defaultTag}, true
		}
	}
	return Message{}, false
}

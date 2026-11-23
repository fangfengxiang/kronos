// Package localization carries and negotiates language preferences across transports.
package localization

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"golang.org/x/text/language"
)

// AcceptLanguageHeader is the common HTTP and gRPC metadata key.
const AcceptLanguageHeader = "accept-language"

// Matcher selects a registered language from ordered preferences and reports
// whether a preference matched. With no match, it returns its default language.
type Matcher interface {
	Match(preferences ...string) (tag string, matched bool)
}

type acceptLanguageKey struct{}
type incomingAcceptLanguageKey struct{}

// WithAcceptLanguage snapshots the outgoing field values for this call.
func WithAcceptLanguage(ctx context.Context, values ...string) context.Context {
	return context.WithValue(ctx, acceptLanguageKey{}, slices.Clone(values))
}

// AcceptLanguage returns a copy of the outgoing field values for this call.
func AcceptLanguage(ctx context.Context) []string {
	values, _ := ctx.Value(acceptLanguageKey{}).([]string)
	return slices.Clone(values)
}

// WithIncomingAcceptLanguage snapshots the field values received by the server.
func WithIncomingAcceptLanguage(ctx context.Context, values ...string) context.Context {
	return context.WithValue(ctx, incomingAcceptLanguageKey{}, slices.Clone(values))
}

type preference struct {
	tag     language.Tag
	quality float32
}

// Negotiate resolves the incoming preferences using matcher. Without an incoming
// request it resolves the values supplied by WithAcceptLanguage. Invalid items,
// zero weights, and wildcards are skipped when ordering explicit tags.
func Negotiate(ctx context.Context, matcher Matcher) (tag string, matched bool) {
	values, incoming := ctx.Value(incomingAcceptLanguageKey{}).([]string)
	if !incoming {
		values, _ = ctx.Value(acceptLanguageKey{}).([]string)
	}
	var preferences []preference
	for _, value := range values {
		for entry := range strings.SplitSeq(value, ",") {
			rangeName, _, _ := strings.Cut(entry, ";")
			if strings.TrimSpace(rangeName) == "*" {
				continue
			}
			tags, qualities, err := language.ParseAcceptLanguage(entry)
			if err != nil || len(tags) == 0 || !(qualities[0] > 0 && qualities[0] <= 1) {
				continue
			}
			preferences = append(preferences, preference{tag: tags[0], quality: qualities[0]})
		}
	}
	slices.SortStableFunc(preferences, func(a, b preference) int { return cmp.Compare(b.quality, a.quality) })
	tags := make([]string, len(preferences))
	for index, preferred := range preferences {
		tags[index] = preferred.tag.String()
	}
	return matcher.Match(tags...)
}

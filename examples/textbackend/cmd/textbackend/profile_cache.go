package main

import (
	"context"
	"net/url"
	"time"
)

type prefixCache interface {
	Get(context.Context, string) (string, bool, error)
	SetIfAbsent(context.Context, string, string, time.Duration) (bool, error)
}

type cachedProfile struct {
	origin prefixSource
	cache  prefixCache
	key    string
	ttl    time.Duration
}

func (profile cachedProfile) Prefix(ctx context.Context) (string, error) {
	const cacheCommandTimeout = 50 * time.Millisecond
	lookupContext, cancelLookup := context.WithTimeout(ctx, cacheCommandTimeout)
	prefix, found, lookupError := profile.cache.Get(lookupContext, profile.key)
	cancelLookup()
	if lookupError == nil && found {
		return prefix, nil
	}
	prefix, err := profile.origin.Prefix(ctx)
	if err != nil || lookupError != nil {
		return prefix, err
	}
	storeContext, cancelStore := context.WithTimeout(ctx, cacheCommandTimeout)
	_, _ = profile.cache.SetIfAbsent(storeContext, profile.key, prefix, profile.ttl)
	cancelStore()
	return prefix, nil
}

func profileCacheKey(namespace, name string) string {
	return "kronos:textbackend:profiles/" + url.PathEscape(namespace) + "/" + url.PathEscape(name)
}

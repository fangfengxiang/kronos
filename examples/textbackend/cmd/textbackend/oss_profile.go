package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/fangfengxiang/kronos/infra/aliyunoss"
)

const maxOSSProfileBytes = 4096

type ossProfile struct {
	bucket *aliyunoss.Bucket
	key    string
}

func openOSSProfile(ctx context.Context, config aliyunoss.Config, key string) (ossProfile, error) {
	bucket, err := aliyunoss.New(config)
	if err != nil {
		return ossProfile{}, fmt.Errorf("configure OSS text profile: %w", err)
	}
	profile := ossProfile{bucket: bucket, key: key}
	startupContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := profile.Prefix(startupContext); err != nil {
		bucket.CloseIdleConnections()
		return ossProfile{}, err
	}
	return profile, nil
}

func (profile ossProfile) Prefix(ctx context.Context) (string, error) {
	body, err := profile.bucket.Open(ctx, profile.key)
	if err != nil {
		return "", fmt.Errorf("open OSS text profile: %w", err)
	}
	defer body.Close()
	content, err := io.ReadAll(io.LimitReader(body, maxOSSProfileBytes+1))
	if err != nil {
		return "", fmt.Errorf("read OSS text profile: %w", err)
	}
	if len(content) > maxOSSProfileBytes || !utf8.Valid(content) {
		return "", errors.New("OSS text profile requires UTF-8 content of at most 4096 bytes")
	}
	return string(content), nil
}

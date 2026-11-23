package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fangfengxiang/kronos/infra/mongodb"
)

type textProfileDocument struct {
	OutputPrefix *string `bson:"output_prefix"`
}

type mongoProfile struct {
	database *mongodb.Database
	profiles *mongodb.Collection[textProfileDocument]
	name     string
}

func openMongoProfile(ctx context.Context, config mongodb.Config, name string) (mongoProfile, error) {
	if strings.TrimSpace(name) == "" {
		return mongoProfile{}, errors.New("MongoDB text profile name must not be blank")
	}
	startupContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	database, err := mongodb.Open(startupContext, config)
	if err != nil {
		return mongoProfile{}, fmt.Errorf("open MongoDB text profile: %w", err)
	}
	profile := mongoProfile{database: database, name: name}
	profile.profiles, err = mongodb.NewCollection[textProfileDocument](database, "text_backend_profiles")
	if err != nil {
		return mongoProfile{}, errors.Join(err, profile.close(ctx))
	}
	if _, err := profile.Prefix(startupContext); err != nil {
		return mongoProfile{}, errors.Join(err, profile.close(ctx))
	}
	return profile, nil
}

func (profile mongoProfile) Prefix(ctx context.Context) (string, error) {
	document, err := profile.profiles.FindByID(ctx, profile.name)
	if err != nil {
		return "", fmt.Errorf("read MongoDB text profile: %w", err)
	}
	if document.OutputPrefix == nil {
		return "", errors.New("MongoDB text profile requires an output_prefix string")
	}
	return *document.OutputPrefix, nil
}

func (profile mongoProfile) close(ctx context.Context) error {
	closeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return profile.database.Close(closeContext)
}

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fangfengxiang/kronos/infra/mysql"
)

type mysqlProfile struct {
	pool *sql.DB
	name string
}

func openMySQLProfile(ctx context.Context, name, dsn string) (mysqlProfile, error) {
	if strings.TrimSpace(name) == "" {
		return mysqlProfile{}, errors.New("MySQL text profile name must not be blank")
	}
	startupContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pool, err := mysql.Open(startupContext, mysql.Config{
		DSN: dsn, MaxOpenConnections: 8, MaxIdleConnections: 4, ConnectionMaxLifetime: 3 * time.Minute,
	})
	if err != nil {
		return mysqlProfile{}, fmt.Errorf("open MySQL text profile: %w", err)
	}
	profile := mysqlProfile{pool: pool, name: name}
	if _, err := profile.Prefix(startupContext); err != nil {
		return mysqlProfile{}, errors.Join(err, pool.Close())
	}
	return profile, nil
}

func (profile mysqlProfile) Prefix(ctx context.Context) (string, error) {
	const query = "SELECT output_prefix FROM text_backend_profiles WHERE name = ?"
	var prefix string
	if err := profile.pool.QueryRowContext(ctx, query, profile.name).Scan(&prefix); err != nil {
		return "", fmt.Errorf("read MySQL text profile: %w", err)
	}
	return prefix, nil
}

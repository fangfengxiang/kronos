// Package mysql opens independent MySQL pools through database/sql.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

var (
	// ErrInvalidConfig indicates that the DSN or a pool setting is invalid.
	ErrInvalidConfig = errors.New("invalid MySQL configuration")
	// ErrUnavailable indicates that the server could not be reached or authenticated.
	ErrUnavailable = errors.New("MySQL connection unavailable")
)

// Config sets the DSN and optional database/sql pool limits. Zero pool settings
// leave the corresponding database/sql defaults in effect.
type Config struct {
	DSN                   string
	MaxOpenConnections    int
	MaxIdleConnections    int
	ConnectionMaxLifetime time.Duration
}

// Open creates and checks a pool using ctx. The caller owns a successful pool
// and can use database/sql directly for statements and transactions.
func Open(ctx context.Context, config Config) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.DSN == "" {
		return nil, fmt.Errorf("%w: missing DSN", ErrInvalidConfig)
	}
	if config.MaxOpenConnections < 0 || config.MaxIdleConnections < 0 || config.ConnectionMaxLifetime < 0 {
		return nil, fmt.Errorf("%w: pool settings must be non-negative", ErrInvalidConfig)
	}
	driverConfig, err := mysqldriver.ParseDSN(config.DSN)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid DSN", ErrInvalidConfig)
	}
	connector, err := mysqldriver.NewConnector(driverConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot configure driver", ErrInvalidConfig)
	}
	pool := sql.OpenDB(connector)
	if config.MaxOpenConnections > 0 {
		pool.SetMaxOpenConns(config.MaxOpenConnections)
	}
	if config.MaxIdleConnections > 0 {
		pool.SetMaxIdleConns(config.MaxIdleConnections)
	}
	pool.SetConnMaxLifetime(config.ConnectionMaxLifetime)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, ErrUnavailable
	}
	return pool, nil
}

// IsDuplicateKey reports whether a statement failed on a duplicate unique key.
func IsDuplicateKey(err error) bool {
	const duplicateEntryCode = 1062
	var failure *mysqldriver.MySQLError
	return errors.As(err, &failure) && failure != nil && failure.Number == duplicateEntryCode
}

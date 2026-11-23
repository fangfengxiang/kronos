// Package mongodb provides typed access to documents in a MongoDB database.
package mongodb

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	// ErrInvalidConfig indicates an invalid connection or collection configuration.
	ErrInvalidConfig = errors.New("invalid MongoDB configuration")
	// ErrUnavailable indicates that the connection or document operation failed.
	ErrUnavailable = errors.New("MongoDB operation unavailable")
	// ErrClosed indicates that an operation was attempted after disconnecting.
	ErrClosed = errors.New("MongoDB database is closed")
	// ErrNotFound indicates that an ID did not match a document.
	ErrNotFound = errors.New("MongoDB document not found")
	// ErrDuplicateKey indicates a conflict with a unique key.
	ErrDuplicateKey = errors.New("MongoDB duplicate key")
)

// Config selects a MongoDB database. The URI controls driver and pool options
// and must use acknowledged writes.
type Config struct {
	URI      string
	Database string
}

// Database owns a MongoDB client for one database. Open constructs it.
type Database struct {
	client *mongo.Client
	native *mongo.Database
}

// Open waits for SRV URI resolution and verifies connectivity within ctx.
// The caller owns the returned database and needs to Close it.
func Open(ctx context.Context, config Config) (*Database, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.URI) == "" || strings.TrimSpace(config.Database) == "" {
		return nil, ErrInvalidConfig
	}
	clientOptions, err := clientOptionsFor(ctx, config.URI)
	if err != nil {
		return nil, err
	}
	if !clientOptions.WriteConcern.Acknowledged() {
		return nil, ErrInvalidConfig
	}
	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	if err := client.Ping(ctx, nil); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = client.Disconnect(cleanup)
		return nil, operationError(ctx, err)
	}
	return &Database{client: client, native: client.Database(config.Database)}, nil
}

func clientOptionsFor(ctx context.Context, uri string) (*options.ClientOptions, error) {
	if !strings.HasPrefix(uri, "mongodb+srv://") || ctx.Done() == nil {
		return options.Client().ApplyURI(uri), ctx.Err()
	}
	// ApplyURI's DNS lookups have no context. Only the caller creates a client,
	// and the buffered result lets the parser finish after the caller leaves.
	resolved := make(chan *options.ClientOptions, 1)
	go func() {
		resolved <- options.Client().ApplyURI(uri)
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case clientOptions := <-resolved:
		return clientOptions, ctx.Err()
	}
}

// Close disconnects the client and releases its connections.
func (database *Database) Close(ctx context.Context) error {
	err := database.client.Disconnect(ctx)
	if errors.Is(err, mongo.ErrClientDisconnected) {
		return nil
	}
	return operationError(ctx, err)
}

func operationError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if contextError := ctx.Err(); contextError != nil {
		return contextError
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, mongo.ErrNoDocuments):
		return ErrNotFound
	case errors.Is(err, mongo.ErrClientDisconnected):
		return ErrClosed
	case mongo.IsDuplicateKeyError(err):
		return ErrDuplicateKey
	default:
		return ErrUnavailable
	}
}

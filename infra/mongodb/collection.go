package mongodb

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Collection provides access to BSON-encodable T values by string _id.
type Collection[T any] struct {
	native *mongo.Collection
}

// NewCollection selects a collection of documents owned by database.
func NewCollection[T any](database *Database, name string) (*Collection[T], error) {
	if database == nil || database.native == nil || strings.TrimSpace(name) == "" || strings.ContainsRune(name, 0) {
		return nil, ErrInvalidConfig
	}
	return &Collection[T]{native: database.native.Collection(name)}, nil
}

// FindByID reads a document. A missing ID returns ErrNotFound and zero T.
func (collection *Collection[T]) FindByID(ctx context.Context, id string) (T, error) {
	var document T
	if err := collection.native.FindOne(ctx, idFilter(id)).Decode(&document); err != nil {
		var zero T
		return zero, operationError(ctx, err)
	}
	return document, nil
}

// ReplaceByID atomically inserts or replaces the document with this ID.
func (collection *Collection[T]) ReplaceByID(ctx context.Context, id string, document T) error {
	_, err := collection.native.ReplaceOne(ctx, idFilter(id), document, options.Replace().SetUpsert(true))
	return operationError(ctx, err)
}

// DeleteByID removes an existing document and reports whether one was removed.
func (collection *Collection[T]) DeleteByID(ctx context.Context, id string) (bool, error) {
	result, err := collection.native.DeleteOne(ctx, idFilter(id))
	if err != nil {
		return false, operationError(ctx, err)
	}
	return result.DeletedCount != 0, nil
}

func idFilter(id string) bson.D {
	return bson.D{{Key: "_id", Value: id}}
}

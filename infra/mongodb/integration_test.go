//go:build mongodb_integration

package mongodb_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fangfengxiang/kronos/infra/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type textProfile struct {
	Prefix string `bson:"output_prefix"`
}

func TestTypedDocumentsMatchNativeMongoDB(t *testing.T) {
	config := testDatabaseConfig()
	if config.URI == "" {
		t.Fatal("set KRONOS_MONGO_TEST_URI for an isolated MongoDB")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	database, err := mongodb.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = database.Close(cleanup)
	})
	nativeClient, err := mongo.Connect(options.Client().ApplyURI(config.URI))
	if err != nil {
		t.Fatal("native driver rejected the test URI")
	}
	collectionName := "profiles_" + rand.Text()
	native := nativeClient.Database(config.Database).Collection(collectionName)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = native.Drop(cleanup)
		_ = nativeClient.Disconnect(cleanup)
	})
	collection, err := mongodb.NewCollection[textProfile](database, collectionName)
	if err != nil {
		t.Fatal(err)
	}
	const id = "greeting"
	if document, err := collection.FindByID(ctx, id); document != (textProfile{}) || !errors.Is(err, mongodb.ErrNotFound) {
		t.Fatalf("missing read = %+v, %v", document, err)
	}
	first := textProfile{Prefix: "你好："}
	if err := collection.ReplaceByID(ctx, id, first); err != nil {
		t.Fatal(err)
	}
	var nativeDocument textProfile
	if err := native.FindOne(ctx, bson.M{"_id": id}).Decode(&nativeDocument); err != nil || nativeDocument != first {
		t.Fatalf("native decoded %v, %v", nativeDocument, err)
	}
	const updated = "更新："
	if err := collection.ReplaceByID(ctx, id, textProfile{Prefix: updated}); err != nil {
		t.Fatal(err)
	}
	if document, err := collection.FindByID(ctx, id); document.Prefix != updated || err != nil {
		t.Fatalf("updated document = %v, %v", document, err)
	}
	if _, err := native.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "output_prefix", Value: 1}}, Options: options.Index().SetUnique(true)}); err != nil {
		t.Fatal(err)
	}
	if err := collection.ReplaceByID(ctx, "duplicate", textProfile{Prefix: updated}); !errors.Is(err, mongodb.ErrDuplicateKey) {
		t.Fatalf("typed duplicate-key write = %v", err)
	}
	if _, err := native.InsertOne(ctx, bson.M{"_id": "native-duplicate", "output_prefix": updated}); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("native duplicate-key write = %v", err)
	}
	queryContext, cancelQuery := context.WithCancel(ctx)
	cancelQuery()
	if _, err := collection.FindByID(queryContext, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
	for _, expected := range []bool{true, false} {
		if deleted, err := collection.DeleteByID(ctx, id); deleted != expected || err != nil {
			t.Fatalf("delete = %v, %v, expected deletion %v", deleted, err, expected)
		}
	}
	if err := native.FindOne(ctx, bson.M{"_id": id}).Err(); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("native read after deletion = %v", err)
	}
	for range 2 {
		if err := database.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := collection.FindByID(ctx, id); !errors.Is(err, mongodb.ErrClosed) {
		t.Fatalf("read after Close = %v", err)
	}
	if err := nativeClient.Ping(ctx, nil); err != nil {
		t.Fatalf("closing an independent object affected the native client: %v", err)
	}
}

func testDatabaseConfig() mongodb.Config {
	database := os.Getenv("KRONOS_MONGO_TEST_DATABASE")
	if database == "" {
		database = "kronos_test"
	}
	return mongodb.Config{URI: os.Getenv("KRONOS_MONGO_TEST_URI"), Database: database}
}

func ExampleOpen() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	database, err := mongodb.Open(ctx, testDatabaseConfig())
	if err != nil {
		panic(err)
	}
	defer database.Close(ctx)
	profiles, err := mongodb.NewCollection[textProfile](database, "example_profiles")
	if err != nil {
		panic(err)
	}
	id := rand.Text()
	if err := profiles.ReplaceByID(ctx, id, textProfile{Prefix: "你好："}); err != nil {
		panic(err)
	}
	defer profiles.DeleteByID(ctx, id)
	profile, err := profiles.FindByID(ctx, id)
	if err != nil {
		panic(err)
	}
	fmt.Println(profile.Prefix)
	// Output: 你好：
}

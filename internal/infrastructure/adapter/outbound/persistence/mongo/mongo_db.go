package mongo

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const operationTimeout = 10 * time.Second

type MongoDB struct {
	Client    *mongo.Client
	Database  *mongo.Database
	Holdings  *mongo.Collection
	Snapshots *mongo.Collection
}

func NewMongoDB(ctx context.Context, uri string, dbName string) (*MongoDB, error) {
	if dbName == "" {
		dbName = "base_wealth"
	}

	// Bounds every operation: a stuck database fails the request instead of holding it open.
	clientOpts := options.Client().ApplyURI(uri).SetTimeout(operationTimeout)
	client, err := mongo.Connect(clientOpts)
	if err != nil {
		return nil, err
	}

	// Ping database with timeout
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()

	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}

	database := client.Database(dbName)
	// Platforms are derived from holdings: a "platforms" collection left by earlier versions is unused.
	db := &MongoDB{
		Client:    client,
		Database:  database,
		Holdings:  database.Collection("holdings"),
		Snapshots: database.Collection("net_worth_snapshots"),
	}

	// Synchronous on purpose: a background goroutine can be frozen on Vercel before it finishes, and
	// the unique index is what turns a second snapshot in the same second into a conflict. Idempotent and cheap once built.
	idxCtx, idxCancel := context.WithTimeout(ctx, 10*time.Second)
	defer idxCancel()
	db.ensureIndexes(idxCtx)

	return db, nil
}

func (db *MongoDB) Close(ctx context.Context) error {
	return db.Client.Disconnect(ctx)
}

func (db *MongoDB) ensureIndexes(ctx context.Context) {
	// Unique Snapshot index: user_id + captured_at
	_, err := db.Snapshots.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "captured_at", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		log.Printf("Warning creating snapshots index: %v", err)
	}

	// Holdings: the user's list and platforms (both by creation) plus the per-platform / per-class lookups.
	_, err = db.Holdings.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "created_at", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "platform_name", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "asset_class", Value: 1},
			},
		},
	})
	if err != nil {
		log.Printf("Warning creating holdings indexes: %v", err)
	}
}

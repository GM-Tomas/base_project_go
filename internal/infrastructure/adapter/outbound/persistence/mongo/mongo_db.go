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
	Debts     *mongo.Collection
	Snapshots *mongo.Collection
	Movements *mongo.Collection
	// Quotas holds per-user counters that transactions keep exact (see MongoQuotaRepository).
	Quotas *mongo.Collection
	// Preferences holds one document per user (_id is the user's id): see MongoPreferencesRepository.
	Preferences *mongo.Collection
	// Platforms is only read, for the types that earlier versions stored (Broker, Wallet...).
	Platforms *mongo.Collection
	// AssetClassSettings and PlatformSettings hold what users set for their classes and platforms (see
	// MongoAssetClassSettingsRepository and MongoPlatformSettingsRepository).
	AssetClassSettings *mongo.Collection
	PlatformSettings   *mongo.Collection
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
	db := &MongoDB{
		Client:      client,
		Database:    database,
		Holdings:    database.Collection("holdings"),
		Debts:       database.Collection("debts"),
		Snapshots:   database.Collection("net_worth_snapshots"),
		Movements:   database.Collection("movements"),
		Quotas:      database.Collection("quotas"),
		Preferences: database.Collection("preferences"),
		Platforms:   database.Collection("platforms"),

		AssetClassSettings: database.Collection("asset_class_settings"),
		PlatformSettings:   database.Collection("platform_settings"),
	}
	if !db.supportsTransactions(pingCtx) {
		log.Println("WARNING: MongoDB isn't a replica set, so it has no transactions: recording a change of value " +
			"(adding, editing or removing a holding or a debt, a movement) will answer 503. Run it as a replica set (see the README).")
	}

	// Synchronous on purpose: a background goroutine can be frozen on Vercel before it finishes, and
	// the unique index is what turns a second snapshot in the same second into a conflict. Idempotent and cheap once built.
	idxCtx, idxCancel := context.WithTimeout(ctx, 10*time.Second)
	defer idxCancel()
	db.ensureIndexes(idxCtx)

	return db, nil
}

// supportsTransactions asks the server what it is: a replica set member (setName) or a mongos router has
// transactions, a standalone mongod doesn't. A failed check is taken as "no", which only costs a warning.
func (db *MongoDB) supportsTransactions(ctx context.Context) bool {
	var hello struct {
		SetName string `bson:"setName"`
		Msg     string `bson:"msg"`
	}
	if err := db.Client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello); err != nil {
		return false
	}
	return hello.SetName != "" || hello.Msg == "isdbgrid"
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

	// Holdings: the user's list and platforms, both read in holdingsOldestFirst order (built from it, so the
	// index always serves it and Mongo never sorts in memory), and the asset classes in use. Older indexes
	// nothing uses now can be dropped, see the README.
	_, err = db.Holdings.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: append(bson.D{{Key: "user_id", Value: 1}}, holdingsOldestFirst...),
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

	// Movements: the activity log newest first (also by period), one holding's, as either end of a
	// transfer, and one debt's (movementsNewestFirst after the filtered fields, so the index serves the sort
	// too).
	_, err = db.Movements.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: append(bson.D{{Key: "user_id", Value: 1}}, movementsNewestFirst...)},
		{Keys: append(bson.D{{Key: "user_id", Value: 1}, {Key: "holding.id", Value: 1}}, movementsNewestFirst...)},
		{Keys: append(bson.D{{Key: "user_id", Value: 1}, {Key: "to_holding.id", Value: 1}}, movementsNewestFirst...)},
		{Keys: append(bson.D{{Key: "user_id", Value: 1}, {Key: "debt.id", Value: 1}}, movementsNewestFirst...)},
	})
	if err != nil {
		log.Printf("Warning creating movements indexes: %v", err)
	}

	// Class and platform settings: the user's (their _id, the user's id and the class's or platform's, keeps
	// one per class or platform), read in _id order.
	for _, coll := range []*mongo.Collection{db.AssetClassSettings, db.PlatformSettings} {
		if _, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "_id", Value: 1}},
		}); err != nil {
			log.Printf("Warning creating %s index: %v", coll.Name(), err)
		}
	}

	// Debts: the user's, in the order they're read (then sorted by balance, which is text).
	_, err = db.Debts.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: 1}, {Key: "_id", Value: 1}},
	})
	if err != nil {
		log.Printf("Warning creating debts index: %v", err)
	}
}

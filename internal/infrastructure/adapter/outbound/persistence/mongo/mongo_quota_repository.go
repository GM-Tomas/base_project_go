package mongo

import (
	"context"
	"errors"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoQuotaRepository keeps one counters document per user ({_id: user_id, <key>: n}). It's meant for use
// inside a transaction: a reservation reads the counter and writes it, so a concurrent reservation for the
// same user hits a write conflict and is retried with the new count.
type MongoQuotaRepository struct {
	coll *mongo.Collection
}

func NewMongoQuotaRepository(db *MongoDB) *MongoQuotaRepository {
	return &MongoQuotaRepository{coll: db.Quotas}
}

var _ outbound.QuotaRepository = (*MongoQuotaRepository)(nil)

func (r *MongoQuotaRepository) Reserve(ctx context.Context, userId model.UserId, key string, n, limit int) (bool, error) {
	user := userId.UUID().String()
	var counters bson.M
	err := r.coll.FindOne(ctx, bson.M{"_id": user}, options.FindOne().SetProjection(bson.M{key: 1})).Decode(&counters)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return false, err
	}
	if count(counters[key])+int64(n) > int64(limit) {
		return false, nil
	}
	_, err = r.coll.UpdateOne(ctx, bson.M{"_id": user}, bson.M{"$inc": bson.M{key: n}}, options.UpdateOne().SetUpsert(true))
	return err == nil, err
}

func (r *MongoQuotaRepository) Release(ctx context.Context, userId model.UserId, key string, n int) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": userId.UUID().String()}, bson.M{"$inc": bson.M{key: -n}})
	return err
}

// count reads a counter as $inc stores it (int32, or int64 once large).
func count(v any) int64 {
	switch n := v.(type) {
	case int32:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

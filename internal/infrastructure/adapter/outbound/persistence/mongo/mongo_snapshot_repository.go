package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type snapshotDoc struct {
	ID            string    `bson:"_id"`
	UserID        string    `bson:"user_id"`
	CapturedAt    time.Time `bson:"captured_at"`
	TotalValueUSD string    `bson:"total_value_usd"`
}

type MongoSnapshotRepository struct {
	coll *mongo.Collection
}

func NewMongoSnapshotRepository(db *MongoDB) *MongoSnapshotRepository {
	return &MongoSnapshotRepository{coll: db.Snapshots}
}

var _ outbound.SnapshotRepository = (*MongoSnapshotRepository)(nil)

func (r *MongoSnapshotRepository) FindAll(
	ctx context.Context,
	userId model.UserId,
	from *time.Time,
	to *time.Time,
) ([]model.NetWorthSnapshot, error) {
	filter := bson.M{"user_id": userId.UUID().String()}

	dateFilter := bson.M{}
	if from != nil {
		dateFilter["$gte"] = *from
	}
	if to != nil {
		dateFilter["$lte"] = *to
	}
	if len(dateFilter) > 0 {
		filter["captured_at"] = dateFilter
	}

	opts := options.Find().SetSort(bson.D{{Key: "captured_at", Value: 1}})
	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []snapshotDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	snapshots := make([]model.NetWorthSnapshot, 0, len(docs))
	for _, doc := range docs {
		s, err := mapDocToSnapshot(doc)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, s)
	}

	return snapshots, nil
}

func (r *MongoSnapshotRepository) Save(
	ctx context.Context,
	snapshot model.NetWorthSnapshot,
) (model.NetWorthSnapshot, error) {
	doc := snapshotDoc{
		ID:            snapshot.Id.UUID().String(),
		UserID:        snapshot.UserId.UUID().String(),
		CapturedAt:    snapshot.CapturedAt,
		TotalValueUSD: snapshot.TotalValue.Amount().StringFixed(2),
	}

	_, err := r.coll.InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return model.NetWorthSnapshot{}, appErrors.DuplicateResourceError{
				Message: "A snapshot already exists for " + snapshot.CapturedAt.Format(time.RFC3339),
			}
		}
		return model.NetWorthSnapshot{}, err
	}

	return snapshot, nil
}

func (r *MongoSnapshotRepository) ExistsAt(
	ctx context.Context,
	userId model.UserId,
	capturedAt time.Time,
) (bool, error) {
	filter := bson.M{
		"user_id":     userId.UUID().String(),
		"captured_at": capturedAt,
	}

	count, err := r.coll.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *MongoSnapshotRepository) FindFirstOfYear(
	ctx context.Context,
	userId model.UserId,
	year int,
) (*model.NetWorthSnapshot, error) {
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	filter := bson.M{
		"user_id":     userId.UUID().String(),
		"captured_at": bson.M{"$gte": yearStart},
	}

	opts := options.FindOne().SetSort(bson.D{{Key: "captured_at", Value: 1}})
	var doc snapshotDoc
	err := r.coll.FindOne(ctx, filter, opts).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}

	s, err := mapDocToSnapshot(doc)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *MongoSnapshotRepository) FindEarliest(
	ctx context.Context,
	userId model.UserId,
) (*model.NetWorthSnapshot, error) {
	filter := bson.M{"user_id": userId.UUID().String()}
	opts := options.FindOne().SetSort(bson.D{{Key: "captured_at", Value: 1}})

	var doc snapshotDoc
	err := r.coll.FindOne(ctx, filter, opts).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}

	s, err := mapDocToSnapshot(doc)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func mapDocToSnapshot(doc snapshotDoc) (model.NetWorthSnapshot, error) {
	id, err := uuid.Parse(doc.ID)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}

	userId, err := uuid.Parse(doc.UserID)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}

	valDec, err := decimal.NewFromString(doc.TotalValueUSD)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}

	m, err := model.NewMoney(valDec)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}

	return model.NetWorthSnapshot{
		Id:         model.SnapshotIdFromUUID(id),
		UserId:     model.NewUserId(userId),
		CapturedAt: doc.CapturedAt,
		TotalValue: m,
	}, nil
}

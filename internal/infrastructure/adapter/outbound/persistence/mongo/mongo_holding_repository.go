package mongo

import (
	"context"
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

type holdingDoc struct {
	ID           string    `bson:"_id"`
	UserID       string    `bson:"user_id"`
	Name         string    `bson:"name"`
	AssetClass   string    `bson:"asset_class"`
	PlatformName string    `bson:"platform_name"`
	ValueUSD     string    `bson:"value_usd"`
	CreatedAt    time.Time `bson:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at"`
}

type MongoHoldingRepository struct {
	coll *mongo.Collection
}

func NewMongoHoldingRepository(db *MongoDB) *MongoHoldingRepository {
	return &MongoHoldingRepository{coll: db.Holdings}
}

var _ outbound.HoldingRepository = (*MongoHoldingRepository)(nil)

func (r *MongoHoldingRepository) FindAll(
	ctx context.Context,
	userId model.UserId,
) ([]model.Holding, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}})
	cursor, err := r.coll.Find(ctx, bson.M{"user_id": userId.UUID().String()}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []holdingDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	holdings := make([]model.Holding, 0, len(docs))
	spellings := platformSpellings{}
	for _, doc := range docs {
		h, err := mapDocToHolding(doc)
		if err != nil {
			return nil, err
		}
		// Every holding names its platform as the platform views do, so clients can match platforms exactly.
		h.Platform, _ = spellings.spell(h.Platform)
		holdings = append(holdings, h)
	}

	return holdings, nil
}

func (r *MongoHoldingRepository) Count(ctx context.Context, userId model.UserId) (int64, error) {
	return r.coll.CountDocuments(ctx, bson.M{"user_id": userId.UUID().String()})
}

func (r *MongoHoldingRepository) Save(
	ctx context.Context,
	holding model.Holding,
) (model.Holding, error) {
	doc := holdingDoc{
		ID:           holding.Id.UUID().String(),
		UserID:       holding.UserId.UUID().String(),
		Name:         holding.Name,
		AssetClass:   holding.AssetClass.Value(),
		PlatformName: holding.Platform.Value(),
		ValueUSD:     holding.Value.Amount().StringFixed(2),
		CreatedAt:    holding.CreatedAt,
		UpdatedAt:    holding.UpdatedAt,
	}

	// The owner is part of the filter: an id that belongs to someone else matches nothing, and the
	// upsert's insert then fails on the duplicate _id instead of overwriting their holding.
	opts := options.Replace().SetUpsert(true)
	_, err := r.coll.ReplaceOne(ctx, bson.M{"_id": doc.ID, "user_id": doc.UserID}, doc, opts)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return model.Holding{}, appErrors.NewDuplicateResourceError("Holding " + doc.ID + " already exists")
		}
		return model.Holding{}, err
	}

	return holding, nil
}

func (r *MongoHoldingRepository) DeleteById(
	ctx context.Context,
	userId model.UserId,
	id model.HoldingId,
) (bool, error) {
	filter := bson.M{
		"_id":     id.UUID().String(),
		"user_id": userId.UUID().String(),
	}

	res, err := r.coll.DeleteOne(ctx, filter)
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (r *MongoHoldingRepository) AssetClassesInUse(
	ctx context.Context,
	userId model.UserId,
) ([]model.AssetClass, error) {
	filter := bson.M{"user_id": userId.UUID().String()}
	res := r.coll.Distinct(ctx, "asset_class", filter)
	var results []string
	if err := res.Decode(&results); err != nil {
		return nil, err
	}

	var classes []model.AssetClass
	for _, str := range results {
		if str == "" {
			continue
		}
		ac, err := model.NewAssetClass(str)
		if err == nil {
			classes = append(classes, ac)
		}
	}

	return classes, nil
}

func mapDocToHolding(doc holdingDoc) (model.Holding, error) {
	id, err := uuid.Parse(doc.ID)
	if err != nil {
		return model.Holding{}, err
	}

	userId, err := uuid.Parse(doc.UserID)
	if err != nil {
		return model.Holding{}, err
	}

	ac, err := model.NewAssetClass(doc.AssetClass)
	if err != nil {
		return model.Holding{}, err
	}

	pn, err := model.NewPlatformName(doc.PlatformName)
	if err != nil {
		return model.Holding{}, err
	}

	valDec, err := decimal.NewFromString(doc.ValueUSD)
	if err != nil {
		return model.Holding{}, err
	}

	m, err := model.NewMoney(valDec)
	if err != nil {
		return model.Holding{}, err
	}

	return model.Holding{
		Id:         model.HoldingIdFromUUID(id),
		UserId:     model.NewUserId(userId),
		Name:       doc.Name,
		AssetClass: ac,
		Platform:   pn,
		Value:      m,
		CreatedAt:  doc.CreatedAt,
		UpdatedAt:  doc.UpdatedAt,
	}, nil
}

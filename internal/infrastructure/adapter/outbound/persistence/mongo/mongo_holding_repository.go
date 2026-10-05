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

type holdingDoc struct {
	ID           string `bson:"_id"`
	UserID       string `bson:"user_id"`
	Name         string `bson:"name"`
	AssetClass   string `bson:"asset_class"`
	PlatformName string `bson:"platform_name"`
	ValueUSD     string `bson:"value_usd"`
	// ExpectedReturnPCT is the yearly return as decimal text ("7.5"); absent when the holding has none.
	ExpectedReturnPCT string    `bson:"expected_return_pct,omitempty"`
	CreatedAt         time.Time `bson:"created_at"`
	UpdatedAt         time.Time `bson:"updated_at"`
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
	docs, err := readHoldings(ctx, r.coll, userId) // every field
	if err != nil {
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

func (r *MongoHoldingRepository) FindById(
	ctx context.Context,
	userId model.UserId,
	id model.HoldingId,
) (*model.Holding, error) {
	var doc holdingDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": id.UUID().String(), "user_id": userId.UUID().String()}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h, err := mapDocToHolding(doc)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

func (r *MongoHoldingRepository) Count(ctx context.Context, userId model.UserId) (int64, error) {
	return r.coll.CountDocuments(ctx, bson.M{"user_id": userId.UUID().String()})
}

func toHoldingDoc(holding model.Holding) holdingDoc {
	return holdingDoc{
		ID:                holding.Id.UUID().String(),
		UserID:            holding.UserId.UUID().String(),
		Name:              holding.Name,
		AssetClass:        holding.AssetClass.Value(),
		PlatformName:      holding.Platform.Value(),
		ValueUSD:          holding.Value.Amount().StringFixed(2),
		ExpectedReturnPCT: returnText(holding.ExpectedReturnPct),
		CreatedAt:         holding.CreatedAt,
		UpdatedAt:         holding.UpdatedAt,
	}
}

func returnText(pct *decimal.Decimal) string {
	if pct == nil {
		return ""
	}
	return pct.String()
}

// readReturn is an expected return as stored. One that can't be read (only writable outside the API) is
// none, rather than failing every read of the holding.
func readReturn(s string) *decimal.Decimal {
	if s == "" {
		return nil
	}
	pct, err := decimal.NewFromString(s)
	if err != nil || pct.Abs().GreaterThan(model.MaxExpectedReturnPct) {
		return nil
	}
	return &pct
}

func (r *MongoHoldingRepository) Save(
	ctx context.Context,
	holding model.Holding,
) (model.Holding, error) {
	doc := toHoldingDoc(holding)

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

// Update replaces the holding only where it already is the user's: no upsert, so it can't bring back a
// holding deleted since it was read.
func (r *MongoHoldingRepository) Update(ctx context.Context, holding model.Holding) (bool, error) {
	doc := toHoldingDoc(holding)
	res, err := r.coll.ReplaceOne(ctx, bson.M{"_id": doc.ID, "user_id": doc.UserID}, doc)
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
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

	// Stored spellings the domain reads as one class (different Unicode forms, stray whitespace from
	// older versions) are listed once.
	var classes []model.AssetClass
	seen := make(map[string]bool, len(results))
	for _, str := range results {
		ac, err := model.NewAssetClass(str)
		if err != nil || seen[ac.Value()] {
			continue
		}
		seen[ac.Value()] = true
		classes = append(classes, ac)
	}

	return classes, nil
}

func (r *MongoHoldingRepository) ExistingIds(
	ctx context.Context,
	userId model.UserId,
	ids []model.HoldingId,
) (map[model.HoldingId]bool, error) {
	existing := make(map[model.HoldingId]bool, len(ids))
	if len(ids) == 0 {
		return existing, nil
	}
	wanted := make([]string, len(ids))
	for i, id := range ids {
		wanted[i] = id.UUID().String()
	}
	cursor, err := r.coll.Find(ctx,
		bson.M{"user_id": userId.UUID().String(), "_id": bson.M{"$in": wanted}},
		options.Find().SetProjection(bson.M{"_id": 1}),
	)
	if err != nil {
		return nil, err
	}
	var docs []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if id, err := uuid.Parse(doc.ID); err == nil {
			existing[model.HoldingIdFromUUID(id)] = true
		}
	}
	return existing, nil
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

	// The name as CreateHolding stores names now. Unlike the asset class and platform, which the views
	// group by and a holding was never read without, names were never validated on read: one CreateHolding
	// would reject (only writable outside the API) is still shown as stored, not turned into an error.
	name, err := model.NormalizeHoldingName(doc.Name)
	if err != nil {
		name = doc.Name
	}

	return model.Holding{
		Id:                model.HoldingIdFromUUID(id),
		UserId:            model.NewUserId(userId),
		Name:              name,
		AssetClass:        ac,
		Platform:          pn,
		Value:             m,
		ExpectedReturnPct: readReturn(doc.ExpectedReturnPCT),
		CreatedAt:         doc.CreatedAt,
		UpdatedAt:         doc.UpdatedAt,
	}, nil
}

// SetExpectedReturns writes the returns, and UpdatedAt, of the user's holdings among these in one round
// trip, touching nothing else of them (not the platform's stored spelling, say).
func (r *MongoHoldingRepository) SetExpectedReturns(
	ctx context.Context,
	userId model.UserId,
	returns map[model.HoldingId]*decimal.Decimal,
	updatedAt time.Time,
) (int, error) {
	if len(returns) == 0 {
		return 0, nil
	}
	writes := make([]mongo.WriteModel, 0, len(returns))
	for id, pct := range returns {
		update := bson.M{"$set": bson.M{"updated_at": updatedAt}}
		if pct == nil {
			update["$unset"] = bson.M{"expected_return_pct": ""}
		} else {
			update["$set"] = bson.M{"updated_at": updatedAt, "expected_return_pct": pct.String()}
		}
		writes = append(writes, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": id.UUID().String(), "user_id": userId.UUID().String()}).
			SetUpdate(update))
	}
	res, err := r.coll.BulkWrite(ctx, writes, options.BulkWrite().SetOrdered(false))
	if err != nil {
		return 0, err
	}
	return int(res.MatchedCount), nil
}

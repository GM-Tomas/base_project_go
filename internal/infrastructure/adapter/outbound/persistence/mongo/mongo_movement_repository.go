package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// movementsNewestFirst is the activity log's order, total thanks to the _id at the end (see MovementCursor).
var movementsNewestFirst = bson.D{{Key: "occurred_at", Value: -1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}

type holdingRefDoc struct {
	ID         string `bson:"id"`
	Name       string `bson:"name"`
	Platform   string `bson:"platform"`
	AssetClass string `bson:"asset_class"`
}

type debtRefDoc struct {
	ID     string `bson:"id"`
	Name   string `bson:"name"`
	Lender string `bson:"lender,omitempty"`
}

type movementDoc struct {
	ID               string         `bson:"_id"`
	UserID           string         `bson:"user_id"`
	Kind             string         `bson:"kind"`
	OccurredAt       time.Time      `bson:"occurred_at"`
	AmountUSD        string         `bson:"amount_usd"`
	FeeUSD           string         `bson:"fee_usd,omitempty"`
	Holding          *holdingRefDoc `bson:"holding,omitempty"`
	ToHolding        *holdingRefDoc `bson:"to_holding,omitempty"`
	Debt             *debtRefDoc    `bson:"debt,omitempty"`
	PreviousValueUSD string         `bson:"previous_value_usd,omitempty"`
	NewValueUSD      string         `bson:"new_value_usd,omitempty"`
	Note             string         `bson:"note,omitempty"`
	CreatedAt        time.Time      `bson:"created_at"`
}

type MongoMovementRepository struct {
	coll *mongo.Collection
}

func NewMongoMovementRepository(db *MongoDB) *MongoMovementRepository {
	return &MongoMovementRepository{coll: db.Movements}
}

var _ outbound.MovementRepository = (*MongoMovementRepository)(nil)

func (r *MongoMovementRepository) Save(ctx context.Context, m model.Movement) error {
	_, err := r.coll.InsertOne(ctx, toMovementDoc(m))
	return err
}

func (r *MongoMovementRepository) FindById(ctx context.Context, userId model.UserId, id model.MovementId) (*model.Movement, error) {
	var doc movementDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": id.UUID().String(), "user_id": userId.UUID().String()}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := mapDocToMovement(doc)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// List reads one page newest first: limit movements, plus one more to know whether another page follows.
func (r *MongoMovementRepository) List(
	ctx context.Context,
	userId model.UserId,
	filter model.MovementFilter,
	after *model.MovementCursor,
	limit int,
) (outbound.MovementPage, error) {
	conditions := bson.A{bson.M{"user_id": userId.UUID().String()}}
	if filter.HoldingId != nil {
		id := filter.HoldingId.UUID().String()
		conditions = append(conditions, bson.M{"$or": bson.A{bson.M{"holding.id": id}, bson.M{"to_holding.id": id}}})
	}
	if filter.DebtId != nil {
		conditions = append(conditions, bson.M{"debt.id": filter.DebtId.UUID().String()})
	}
	if len(filter.Kinds) > 0 {
		kinds := make(bson.A, len(filter.Kinds))
		for i, k := range filter.Kinds {
			kinds[i] = string(k)
		}
		conditions = append(conditions, bson.M{"kind": bson.M{"$in": kinds}})
	}
	if filter.From != nil {
		conditions = append(conditions, bson.M{"occurred_at": bson.M{"$gte": *filter.From}})
	}
	if filter.To != nil {
		conditions = append(conditions, bson.M{"occurred_at": bson.M{"$lte": *filter.To}})
	}
	if after != nil {
		// Strictly after the cursor in (occurred_at, created_at, _id) descending order.
		id := after.Id.UUID().String()
		conditions = append(conditions, bson.M{"$or": bson.A{
			bson.M{"occurred_at": bson.M{"$lt": after.OccurredAt}},
			bson.M{"occurred_at": after.OccurredAt, "created_at": bson.M{"$lt": after.CreatedAt}},
			bson.M{"occurred_at": after.OccurredAt, "created_at": after.CreatedAt, "_id": bson.M{"$lt": id}},
		}})
	}

	cursor, err := r.coll.Find(ctx, bson.M{"$and": conditions},
		options.Find().SetSort(movementsNewestFirst).SetLimit(int64(limit+1)))
	if err != nil {
		return outbound.MovementPage{}, err
	}
	var docs []movementDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return outbound.MovementPage{}, err
	}

	page := outbound.MovementPage{Items: make([]model.Movement, 0, min(len(docs), limit))}
	for i, doc := range docs {
		if i == limit {
			last := page.Items[limit-1]
			page.Next = &model.MovementCursor{OccurredAt: last.OccurredAt, CreatedAt: last.CreatedAt, Id: last.Id}
			break
		}
		m, err := mapDocToMovement(doc)
		if err != nil {
			return outbound.MovementPage{}, err
		}
		page.Items = append(page.Items, m)
	}
	return page, nil
}

func (r *MongoMovementRepository) DeleteById(ctx context.Context, userId model.UserId, id model.MovementId) (bool, error) {
	res, err := r.coll.DeleteOne(ctx, bson.M{"_id": id.UUID().String(), "user_id": userId.UUID().String()})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func toHoldingRefDoc(ref *model.HoldingRef) *holdingRefDoc {
	if ref == nil {
		return nil
	}
	return &holdingRefDoc{ID: ref.Id.UUID().String(), Name: ref.Name, Platform: ref.Platform.Value(), AssetClass: ref.AssetClass.Value()}
}

func optionalAmount(m *model.Money) string {
	if m == nil {
		return ""
	}
	return m.Amount().StringFixed(2)
}

func toMovementDoc(m model.Movement) movementDoc {
	doc := movementDoc{
		ID:               m.Id.UUID().String(),
		UserID:           m.UserId.UUID().String(),
		Kind:             string(m.Kind),
		OccurredAt:       m.OccurredAt,
		AmountUSD:        m.Amount.Amount().StringFixed(2),
		Holding:          toHoldingRefDoc(m.Holding),
		ToHolding:        toHoldingRefDoc(m.ToHolding),
		PreviousValueUSD: optionalAmount(m.PreviousValue),
		NewValueUSD:      optionalAmount(m.NewValue),
		Note:             m.Note,
		CreatedAt:        m.CreatedAt,
	}
	if m.Kind == model.MovementTransfer {
		doc.FeeUSD = m.Fee.Amount().StringFixed(2)
	}
	if d := m.Debt; d != nil {
		doc.Debt = &debtRefDoc{ID: d.Id.UUID().String(), Name: d.Name, Lender: d.Lender}
	}
	return doc
}

func readMoney(s string) (model.Money, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return model.Money{}, err
	}
	return model.NewMoney(d)
}

func readOptionalMoney(s string) (*model.Money, error) {
	if s == "" {
		return nil, nil
	}
	m, err := readMoney(s)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// readHoldingRef reads a holding as a movement remembers it. Names were valid when stored; one written
// outside the API is shown as stored rather than failing the whole activity log.
func readHoldingRef(doc *holdingRefDoc) (*model.HoldingRef, error) {
	if doc == nil {
		return nil, nil
	}
	id, err := uuid.Parse(doc.ID)
	if err != nil {
		return nil, err
	}
	ref := &model.HoldingRef{Id: model.HoldingIdFromUUID(id), Name: doc.Name}
	if p, err := model.NewPlatformName(doc.Platform); err == nil {
		ref.Platform = p
	}
	if ac, err := model.NewAssetClass(doc.AssetClass); err == nil {
		ref.AssetClass = ac
	}
	return ref, nil
}

func mapDocToMovement(doc movementDoc) (model.Movement, error) {
	id, err := uuid.Parse(doc.ID)
	if err != nil {
		return model.Movement{}, err
	}
	userId, err := uuid.Parse(doc.UserID)
	if err != nil {
		return model.Movement{}, err
	}
	kind, err := model.ParseMovementKind(doc.Kind)
	if err != nil {
		return model.Movement{}, err
	}
	amount, err := readMoney(doc.AmountUSD)
	if err != nil {
		return model.Movement{}, err
	}
	fee := model.ZeroMoney
	if doc.FeeUSD != "" {
		if fee, err = readMoney(doc.FeeUSD); err != nil {
			return model.Movement{}, err
		}
	}
	holding, err := readHoldingRef(doc.Holding)
	if err != nil {
		return model.Movement{}, err
	}
	toHolding, err := readHoldingRef(doc.ToHolding)
	if err != nil {
		return model.Movement{}, err
	}
	previous, err := readOptionalMoney(doc.PreviousValueUSD)
	if err != nil {
		return model.Movement{}, err
	}
	next, err := readOptionalMoney(doc.NewValueUSD)
	if err != nil {
		return model.Movement{}, err
	}
	var debt *model.DebtRef
	if doc.Debt != nil {
		debtId, err := uuid.Parse(doc.Debt.ID)
		if err != nil {
			return model.Movement{}, err
		}
		debt = &model.DebtRef{Id: model.DebtIdFromUUID(debtId), Name: doc.Debt.Name, Lender: doc.Debt.Lender}
	}
	return model.Movement{
		Id:            model.MovementIdFromUUID(id),
		UserId:        model.NewUserId(userId),
		Kind:          kind,
		OccurredAt:    doc.OccurredAt,
		Amount:        amount,
		Fee:           fee,
		Holding:       holding,
		ToHolding:     toHolding,
		Debt:          debt,
		PreviousValue: previous,
		NewValue:      next,
		Note:          doc.Note,
		CreatedAt:     doc.CreatedAt,
	}, nil
}

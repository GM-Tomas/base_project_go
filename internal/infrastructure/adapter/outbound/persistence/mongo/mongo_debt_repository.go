package mongo

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// debtDoc stores amounts and rates as decimal text, like holdings; the optional terms are left out when not
// given.
type debtDoc struct {
	ID                string    `bson:"_id"`
	UserID            string    `bson:"user_id"`
	Name              string    `bson:"name"`
	Lender            string    `bson:"lender,omitempty"`
	Kind              string    `bson:"kind"`
	BalanceUSD        string    `bson:"balance_usd"`
	InterestRatePct   string    `bson:"interest_rate_pct,omitempty"`
	MonthlyPaymentUSD string    `bson:"monthly_payment_usd,omitempty"`
	DueDay            *int      `bson:"due_day,omitempty"`
	Notes             string    `bson:"notes,omitempty"`
	CreatedAt         time.Time `bson:"created_at"`
	UpdatedAt         time.Time `bson:"updated_at"`
}

type MongoDebtRepository struct {
	coll *mongo.Collection
}

func NewMongoDebtRepository(db *MongoDB) *MongoDebtRepository {
	return &MongoDebtRepository{coll: db.Debts}
}

var _ outbound.DebtRepository = (*MongoDebtRepository)(nil)

// FindAll reads the user's debts and sorts them, largest balance first, then by name as a person reads it
// (balances are text, so Mongo can't sort them by amount; a user has at most model.MaxDebtsPerUser).
func (r *MongoDebtRepository) FindAll(ctx context.Context, userId model.UserId) ([]model.Debt, error) {
	cursor, err := r.coll.Find(ctx, bson.M{"user_id": userId.UUID().String()},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []debtDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	debts := make([]model.Debt, 0, len(docs))
	for _, doc := range docs {
		d, err := mapDocToDebt(doc)
		if err != nil {
			return nil, err
		}
		debts = append(debts, d)
	}
	slices.SortStableFunc(debts, func(a, b model.Debt) int {
		return cmp.Or(b.Balance.Cmp(a.Balance), strings.Compare(sortName(a.Name), sortName(b.Name)))
	})
	return debts, nil
}

func (r *MongoDebtRepository) FindById(ctx context.Context, userId model.UserId, id model.DebtId) (*model.Debt, error) {
	var doc debtDoc
	err := r.coll.FindOne(ctx, bson.M{"_id": id.UUID().String(), "user_id": userId.UUID().String()}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d, err := mapDocToDebt(doc)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *MongoDebtRepository) Count(ctx context.Context, userId model.UserId) (int64, error) {
	return r.coll.CountDocuments(ctx, bson.M{"user_id": userId.UUID().String()})
}

func (r *MongoDebtRepository) Insert(ctx context.Context, debt model.Debt) error {
	_, err := r.coll.InsertOne(ctx, toDebtDoc(debt))
	return err
}

// Update replaces the debt only where it already is the user's: no upsert, so it can't bring back a debt
// deleted since it was read.
func (r *MongoDebtRepository) Update(ctx context.Context, debt model.Debt) (bool, error) {
	doc := toDebtDoc(debt)
	res, err := r.coll.ReplaceOne(ctx, bson.M{"_id": doc.ID, "user_id": doc.UserID}, doc)
	if err != nil {
		return false, err
	}
	return res.MatchedCount > 0, nil
}

func (r *MongoDebtRepository) DeleteById(ctx context.Context, userId model.UserId, id model.DebtId) (bool, error) {
	res, err := r.coll.DeleteOne(ctx, bson.M{"_id": id.UUID().String(), "user_id": userId.UUID().String()})
	if err != nil {
		return false, err
	}
	return res.DeletedCount > 0, nil
}

func (r *MongoDebtRepository) ExistingIds(ctx context.Context, userId model.UserId, ids []model.DebtId) (map[model.DebtId]bool, error) {
	existing := make(map[model.DebtId]bool, len(ids))
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
		if u, err := uuid.Parse(doc.ID); err == nil {
			existing[model.DebtIdFromUUID(u)] = true
		}
	}
	return existing, nil
}

func toDebtDoc(d model.Debt) debtDoc {
	doc := debtDoc{
		ID:                d.Id.UUID().String(),
		UserID:            d.UserId.UUID().String(),
		Name:              d.Name,
		Lender:            d.Lender,
		Kind:              string(d.Kind),
		BalanceUSD:        d.Balance.Amount().StringFixed(2),
		MonthlyPaymentUSD: optionalAmount(d.MonthlyPayment),
		DueDay:            d.DueDay,
		Notes:             d.Notes,
		CreatedAt:         d.CreatedAt,
		UpdatedAt:         d.UpdatedAt,
	}
	if d.InterestRatePct != nil {
		doc.InterestRatePct = d.InterestRatePct.StringFixed(2)
	}
	return doc
}

func mapDocToDebt(doc debtDoc) (model.Debt, error) {
	id, err := uuid.Parse(doc.ID)
	if err != nil {
		return model.Debt{}, err
	}
	userId, err := uuid.Parse(doc.UserID)
	if err != nil {
		return model.Debt{}, err
	}
	kind, err := model.ParseDebtKind(doc.Kind)
	if err != nil {
		return model.Debt{}, err
	}
	balance, err := readMoney(doc.BalanceUSD)
	if err != nil {
		return model.Debt{}, err
	}
	payment, err := readOptionalMoney(doc.MonthlyPaymentUSD)
	if err != nil {
		return model.Debt{}, err
	}
	d := model.Debt{
		Id: model.DebtIdFromUUID(id), UserId: model.NewUserId(userId), Name: doc.Name, Lender: doc.Lender,
		Kind: kind, Balance: balance, MonthlyPayment: payment, DueDay: doc.DueDay, Notes: doc.Notes,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}
	if doc.InterestRatePct != "" {
		rate, err := decimal.NewFromString(doc.InterestRatePct)
		if err != nil {
			return model.Debt{}, err
		}
		d.InterestRatePct = &rate
	}
	return d, nil
}

package mongo

import (
	"context"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func debtOf(user model.UserId, name string, balance float64, created time.Time) model.Debt {
	return model.Debt{
		Id: model.NewDebtId(), UserId: user, Name: name, Kind: model.DebtOther,
		Balance: model.MustMoneyFromFloat(balance), CreatedAt: created, UpdatedAt: created,
	}
}

func TestDebtRepository(t *testing.T) {
	db := testDB(t)
	repo := NewMongoDebtRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	rate, payment, day := decimal.RequireFromString("65.5"), model.MustMoneyFromFloat(300), 10
	visa := debtOf(user, "Visa", 1250.4, at(1))
	visa.Lender, visa.Kind, visa.Notes = "Galicia", model.DebtCreditCard, "0% until March"
	visa.InterestRatePct, visa.MonthlyPayment, visa.DueDay = &rate, &payment, &day
	amex := debtOf(user, "amex", 1250.4, at(2))
	mortgage := debtOf(user, "Mortgage", 80000, at(3))
	for _, d := range []model.Debt{visa, amex, mortgage, debtOf(other, "Theirs", 1e6, at(1))} {
		require.NoError(t, repo.Insert(ctx, d))
	}

	// Largest balance first; equal balances by name as a person reads it.
	all, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	var names []string
	for _, d := range all {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"Mortgage", "amex", "Visa"}, names)

	// Every field comes back as stored; optional ones stay absent.
	got, err := repo.FindById(ctx, user, visa.Id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Galicia", got.Lender)
	assert.Equal(t, model.DebtCreditCard, got.Kind)
	assert.Equal(t, "1250.40", got.Balance.String())
	assert.Equal(t, "65.50", got.InterestRatePct.StringFixed(2))
	assert.Equal(t, "300.00", got.MonthlyPayment.String())
	assert.Equal(t, 10, *got.DueDay)
	assert.Equal(t, "0% until March", got.Notes)
	assert.Equal(t, at(1), got.CreatedAt)
	plain, err := repo.FindById(ctx, user, amex.Id)
	require.NoError(t, err)
	assert.Nil(t, plain.InterestRatePct)
	assert.Nil(t, plain.MonthlyPayment)
	assert.Nil(t, plain.DueDay)
	assert.Empty(t, plain.Lender)
	var raw bson.M
	require.NoError(t, db.Debts.FindOne(ctx, bson.M{"_id": amex.Id.String()}).Decode(&raw))
	for _, field := range []string{"lender", "interest_rate_pct", "monthly_payment_usd", "due_day", "notes"} {
		assert.NotContains(t, raw, field, "absent when not given")
	}

	// Someone else's is not found, nor updated, nor deleted.
	theirs, err := repo.FindAll(ctx, other)
	require.NoError(t, err)
	require.Len(t, theirs, 1)
	none, err := repo.FindById(ctx, user, theirs[0].Id)
	require.NoError(t, err)
	assert.Nil(t, none)
	stolen := theirs[0]
	stolen.UserId = user
	updated, err := repo.Update(ctx, stolen)
	require.NoError(t, err)
	assert.False(t, updated)
	deleted, err := repo.DeleteById(ctx, user, theirs[0].Id)
	require.NoError(t, err)
	assert.False(t, deleted)

	// Updated in place, with optional terms cleared; never brought back once deleted.
	got.InterestRatePct, got.Lender, got.Balance = nil, "", model.MustMoneyFromFloat(950)
	updated, err = repo.Update(ctx, *got)
	require.NoError(t, err)
	assert.True(t, updated)
	again, err := repo.FindById(ctx, user, visa.Id)
	require.NoError(t, err)
	assert.Nil(t, again.InterestRatePct)
	assert.Equal(t, "950.00", again.Balance.String())
	deleted, err = repo.DeleteById(ctx, user, visa.Id)
	require.NoError(t, err)
	assert.True(t, deleted)
	updated, err = repo.Update(ctx, *again)
	require.NoError(t, err)
	assert.False(t, updated, "no upsert")

	n, err := repo.Count(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	existing, err := repo.ExistingIds(ctx, user, []model.DebtId{visa.Id, amex.Id, theirs[0].Id})
	require.NoError(t, err)
	assert.Equal(t, map[model.DebtId]bool{amex.Id: true}, existing)
	existing, err = repo.ExistingIds(ctx, user, nil)
	require.NoError(t, err)
	assert.Empty(t, existing)
}

func TestDebtRepository_Errors(t *testing.T) {
	db := testDB(t)
	repo := NewMongoDebtRepository(db)
	user := newUser()
	d := debtOf(user, "x", 1, at(1))

	_, err := repo.FindAll(cancelled(), user)
	assert.Error(t, err)
	_, err = repo.FindById(cancelled(), user, d.Id)
	assert.Error(t, err)
	_, err = repo.Count(cancelled(), user)
	assert.Error(t, err)
	assert.Error(t, repo.Insert(cancelled(), d))
	_, err = repo.Update(cancelled(), d)
	assert.Error(t, err)
	_, err = repo.DeleteById(cancelled(), user, d.Id)
	assert.Error(t, err)
	_, err = repo.ExistingIds(cancelled(), user, []model.DebtId{d.Id})
	assert.Error(t, err)

	id := uuid.NewString()
	insertRaw(t, db, "debts", bson.M{"_id": id, "user_id": user.String(), "name": "x", "kind": "OTHER", "balance_usd": "abc", "created_at": at(1)})
	_, err = repo.FindAll(context.Background(), user)
	assert.Error(t, err)
	parsed, _ := model.ParseDebtId(id)
	_, err = repo.FindById(context.Background(), user, parsed)
	assert.Error(t, err)
}

func TestMapDocToDebt_RejectsCorruptDocs(t *testing.T) {
	valid := debtDoc{ID: uuid.NewString(), UserID: uuid.NewString(), Name: "Visa", Kind: "LOAN", BalanceUSD: "1.00"}
	_, err := mapDocToDebt(valid)
	require.NoError(t, err)

	for name, corrupt := range map[string]func(*debtDoc){
		"id":       func(d *debtDoc) { d.ID = "x" },
		"user id":  func(d *debtDoc) { d.UserID = "x" },
		"kind":     func(d *debtDoc) { d.Kind = "CARD" },
		"balance":  func(d *debtDoc) { d.BalanceUSD = "-1" },
		"payment":  func(d *debtDoc) { d.MonthlyPaymentUSD = "abc" },
		"interest": func(d *debtDoc) { d.InterestRatePct = "abc" },
	} {
		t.Run(name, func(t *testing.T) {
			doc := valid
			corrupt(&doc)
			_, err := mapDocToDebt(doc)
			assert.Error(t, err)
		})
	}
}

func TestMovementRepository_RemembersTheDebt(t *testing.T) {
	db := testDB(t)
	repo := NewMongoMovementRepository(db)
	ctx := context.Background()
	user := newUser()
	savings := holding(user, "Savings", "Cash", "Bank", 100, at(1))
	visa, amex := model.NewDebtId(), model.NewDebtId()

	payment := movementOf(user, model.MovementDebtPayment, &savings, at(2))
	payment.Debt = &model.DebtRef{Id: visa, Name: "Visa", Lender: "Galicia"}
	interest := movementOf(user, model.MovementDebtInterest, &savings, at(3))
	interest.Holding, interest.Debt = nil, &model.DebtRef{Id: amex, Name: "Amex"}
	for _, m := range []model.Movement{payment, interest, movementOf(user, model.MovementGain, &savings, at(4))} {
		require.NoError(t, repo.Save(ctx, m))
	}

	got, err := repo.FindById(ctx, user, payment.Id)
	require.NoError(t, err)
	assert.Equal(t, payment.Debt, got.Debt)
	assert.Equal(t, savings.Id, got.Holding.Id)
	var raw bson.M
	require.NoError(t, db.Movements.FindOne(ctx, bson.M{"_id": interest.Id.String()}).Decode(&raw))
	assert.Equal(t, bson.D{{Key: "id", Value: amex.String()}, {Key: "name", Value: "Amex"}}, raw["debt"], "no lender stored when there's none")

	page, err := repo.List(ctx, user, model.MovementFilter{DebtId: &visa}, nil, 50)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, payment.Id, page.Items[0].Id)

	insertRaw(t, db, "movements", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "kind": "DEBT_INTEREST",
		"occurred_at": at(9), "created_at": at(9), "amount_usd": "1.00", "debt": bson.M{"id": "x", "name": "Bad"}})
	_, err = repo.List(ctx, user, model.MovementFilter{}, nil, 50)
	assert.Error(t, err, "a debt id that isn't one")
}

func TestSnapshotRepository_KeepsWhatWasOwnedAndOwed(t *testing.T) {
	db := testDB(t)
	repo := NewMongoSnapshotRepository(db)
	ctx := context.Background()
	user := newUser()

	owing := model.NewNetWorthSnapshot(model.NewSnapshotId(), user, at(2), model.MustMoneyFromFloat(1000), model.MustMoneyFromFloat(1500.5))
	_, err := repo.Save(ctx, owing)
	require.NoError(t, err)
	var raw bson.M
	require.NoError(t, db.Snapshots.FindOne(ctx, bson.M{"_id": owing.Id.String()}).Decode(&raw))
	assert.Equal(t, "-500.50", raw["total_value_usd"])
	assert.Equal(t, "1000.00", raw["assets_usd"])
	assert.Equal(t, "1500.50", raw["debts_usd"])

	// From before debts: what was owned was the total, and nothing was owed.
	old := uuid.NewString()
	insertRaw(t, db, "net_worth_snapshots", bson.M{"_id": old, "user_id": user.String(), "captured_at": at(1), "total_value_usd": "800.00"})

	all, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, old, all[0].Id.String())
	assert.Equal(t, "800.00", all[0].TotalValue.String())
	assert.Equal(t, "800.00", all[0].Assets.String())
	assert.True(t, all[0].Debts.IsZero())
	assert.Equal(t, owing, all[1])
}

func TestWealthAggregation_CountsWhatIsOwed(t *testing.T) {
	db := testDB(t)
	agg := NewMongoWealthAggregationAdapter(db)
	debts := NewMongoDebtRepository(db)
	holdings := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()

	_, err := holdings.Save(ctx, holding(user, "Savings", "Cash", "Bank", 1000, at(1)))
	require.NoError(t, err)
	payment := model.MustMoneyFromFloat(300)
	visa := debtOf(user, "Visa", 1250, at(1))
	visa.MonthlyPayment = &payment
	for _, d := range []model.Debt{visa, debtOf(user, "Mom", 500, at(2)), debtOf(newUser(), "Theirs", 9999, at(1))} {
		require.NoError(t, debts.Insert(ctx, d))
	}
	// A balance that can't be read isn't counted, as with holdings.
	insertRaw(t, db, "debts", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "balance_usd": "abc", "monthly_payment_usd": "5.00"})
	insertRaw(t, db, "debts", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "balance_usd": "1.00", "monthly_payment_usd": "abc"})

	totals, err := agg.Totals(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, "1000.00", totals.Assets.String())
	assert.Equal(t, "1751.00", totals.Debts.String())
	assert.Equal(t, "-751.00", totals.NetWorth().String())

	b := breakdown(t, db, ctx, user)
	assert.Equal(t, "1751.00", b.Debts.Balance.String())
	assert.Equal(t, 3, b.Debts.Count)
	assert.Equal(t, "300.00", b.Debts.MonthlyPayment.String())

	insertRaw(t, db, "debts", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "balance_usd": 5})
	_, err = agg.Totals(ctx, user)
	assert.Error(t, err)
	_, err = agg.Breakdown(ctx, user)
	assert.Error(t, err)
}

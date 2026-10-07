package mongo

import (
	"context"
	"errors"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func pctOf(v string) *decimal.Decimal {
	d := decimal.RequireFromString(v)
	return &d
}

func TestHoldingRepository_KeepsTheExpectedReturn(t *testing.T) {
	db := testDB(t)
	repo := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()

	voo := holding(user, "VOO", "Index Fund", "IBKR", 1000, at(1))
	voo.ExpectedReturnPct = pctOf("7.5")
	cash := holding(user, "Cash", "Cash", "Bank", 500, at(2))
	_, err := repo.Save(ctx, voo)
	require.NoError(t, err)
	_, err = repo.Save(ctx, cash)
	require.NoError(t, err)

	got, err := repo.FindById(ctx, user, voo.Id)
	require.NoError(t, err)
	assert.Equal(t, "7.5", got.ExpectedReturnPct.String())
	got, err = repo.FindById(ctx, user, cash.Id)
	require.NoError(t, err)
	assert.Nil(t, got.ExpectedReturnPct)
	// None isn't stored at all.
	var raw bson.M
	require.NoError(t, db.Holdings.FindOne(ctx, bson.M{"_id": cash.Id.String()}).Decode(&raw))
	assert.NotContains(t, raw, "expected_return_pct")

	// Cleared by an update.
	voo.ExpectedReturnPct = nil
	ok, err := repo.Update(ctx, voo)
	require.NoError(t, err)
	assert.True(t, ok)
	got, _ = repo.FindById(ctx, user, voo.Id)
	assert.Nil(t, got.ExpectedReturnPct)

	// One that can't be read (only writable outside the API) reads as none.
	for _, stored := range []string{"lots", "250"} {
		_, err := db.Holdings.UpdateOne(ctx, bson.M{"_id": cash.Id.String()}, bson.M{"$set": bson.M{"expected_return_pct": stored}})
		require.NoError(t, err)
		got, err = repo.FindById(ctx, user, cash.Id)
		require.NoError(t, err)
		assert.Nil(t, got.ExpectedReturnPct, stored)
	}
}

func TestHoldingRepository_SetsExpectedReturnsInOneGo(t *testing.T) {
	db := testDB(t)
	repo := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	a := holding(user, "A", "Cash", "bank", 100, at(1))
	b := holding(user, "B", "Cash", "Bank", 100, at(2)) // spelled differently: it stays as stored
	b.ExpectedReturnPct = pctOf("3")
	theirs := holding(other, "Theirs", "Cash", "Bank", 100, at(3))
	for _, h := range []model.Holding{a, b, theirs} {
		_, err := repo.Save(ctx, h)
		require.NoError(t, err)
	}

	found, err := repo.SetExpectedReturns(ctx, user, map[model.HoldingId]*decimal.Decimal{
		a.Id: pctOf("8.25"), b.Id: nil, theirs.Id: pctOf("50"), model.NewHoldingId(): pctOf("1"),
	}, at(10))
	require.NoError(t, err)
	assert.Equal(t, 2, found, "only the user's")

	got, _ := repo.FindById(ctx, user, a.Id)
	assert.Equal(t, "8.25", got.ExpectedReturnPct.String())
	assert.Equal(t, at(10), got.UpdatedAt)
	got, _ = repo.FindById(ctx, user, b.Id)
	assert.Nil(t, got.ExpectedReturnPct)
	assert.Equal(t, "Bank", got.Platform.Value())
	var raw bson.M
	require.NoError(t, db.Holdings.FindOne(ctx, bson.M{"_id": a.Id.String()}).Decode(&raw))
	assert.Equal(t, "bank", raw["platform_name"], "nothing else is rewritten")
	got, _ = repo.FindById(ctx, other, theirs.Id)
	assert.Nil(t, got.ExpectedReturnPct)

	found, err = repo.SetExpectedReturns(ctx, user, nil, at(11))
	require.NoError(t, err)
	assert.Zero(t, found)
	_, err = repo.SetExpectedReturns(cancelled(), user, map[model.HoldingId]*decimal.Decimal{a.Id: nil}, at(11))
	assert.Error(t, err)
}

func TestHoldingRepository_ExpectedReturnsAreAllOrNothingInATransaction(t *testing.T) {
	db := testDB(t)
	tx := NewMongoTransactionManager(db)
	repo := NewMongoHoldingRepository(db)
	ctx := context.Background()
	user := newUser()
	h := holding(user, "A", "Cash", "Bank", 100, at(1))
	_, err := repo.Save(ctx, h)
	require.NoError(t, err)

	boom := errors.New("boom")
	err = tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if _, err := repo.SetExpectedReturns(ctx, user, map[model.HoldingId]*decimal.Decimal{h.Id: pctOf("9")}, at(2)); err != nil {
			return err
		}
		return boom
	})
	assert.ErrorIs(t, err, boom)
	got, _ := repo.FindById(ctx, user, h.Id)
	assert.Nil(t, got.ExpectedReturnPct, "rolled back")
}

func TestWealthAggregation_ReadsEachHoldingsReturn(t *testing.T) {
	db := testDB(t)
	repo := NewMongoHoldingRepository(db)
	agg := NewMongoWealthAggregationAdapter(db)
	ctx := context.Background()
	user := newUser()

	voo := holding(user, "VOO", "Index Fund", "IBKR", 600, at(1))
	voo.ExpectedReturnPct = pctOf("8")
	for _, h := range []model.Holding{voo, holding(user, "Cash", "Cash", "Bank", 400, at(2))} {
		_, err := repo.Save(ctx, h)
		require.NoError(t, err)
	}
	// An amount that can't be read isn't counted, return or not.
	insertRaw(t, db, "holdings", bson.M{
		"_id": uuid.NewString(), "user_id": user.String(), "name": "bad", "asset_class": "Cash", "platform_name": "Bank",
		"value_usd": "lots", "expected_return_pct": "50", "created_at": at(3), "updated_at": at(3),
	})

	totals, err := agg.Totals(ctx, user)
	require.NoError(t, err)
	require.Len(t, totals.Returns, 2)
	assert.Equal(t, "600.00", totals.Returns[0].Value.String())
	assert.Equal(t, "8", totals.Returns[0].Pct.String())
	assert.Nil(t, totals.Returns[1].Pct)

	b := breakdown(t, db, ctx, user)
	assert.Equal(t, totals.Returns, b.Returns)
}

func TestPreferencesRepository(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPreferencesRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()

	got, err := repo.Find(ctx, user)
	require.NoError(t, err)
	assert.Nil(t, got, "never saved")

	preferences := model.DefaultPreferences()
	preferences.Estimate.Contribution = model.MustMoneyFromFloat(1234.5)
	preferences.Estimate.Years = 30
	preferences.Estimate.YieldMode = model.YieldModeCustom
	preferences.Estimate.CustomYieldPct = decimal.RequireFromString("-2.75")
	preferences.Estimate.Milestones = []model.Money{model.MustMoneyFromFloat(1e6)}
	preferences.Estimate.InflationPct = decimal.RequireFromString("3.5")
	preferences.Estimate.ContributionGrowthPct = decimal.NewFromInt(5)
	preferences.AutoSnapshot = model.AutoSnapshotMonthly
	preferences.DefaultView = "debts"
	preferences.HistoryPeriod = "ALL"
	require.NoError(t, repo.Save(ctx, user, preferences))

	got, err = repo.Find(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, model.AutoSnapshotMonthly, got.AutoSnapshot)
	assert.Equal(t, model.StartView("debts"), got.DefaultView)
	assert.Equal(t, model.HistoryPeriod("ALL"), got.HistoryPeriod)
	assert.Equal(t, "1234.50", got.Estimate.Contribution.String())
	assert.Equal(t, 30, got.Estimate.Years)
	assert.Equal(t, model.YieldModeCustom, got.Estimate.YieldMode)
	assert.Equal(t, "-2.75", got.Estimate.CustomYieldPct.String())
	assert.Equal(t, []string{"1000000.00"}, []string{got.Estimate.Milestones[0].String()})
	assert.Equal(t, "3.5", got.Estimate.InflationPct.String())
	assert.Equal(t, "5", got.Estimate.ContributionGrowthPct.String())
	none, err := repo.Find(ctx, other)
	require.NoError(t, err)
	assert.Nil(t, none, "each user has their own")

	// Saving again replaces them; no milestones stay none.
	preferences.Estimate.Milestones = []model.Money{}
	require.NoError(t, repo.Save(ctx, user, preferences))
	got, _ = repo.Find(ctx, user)
	assert.Empty(t, got.Estimate.Milestones)
	count, err := db.Preferences.CountDocuments(ctx, bson.M{"_id": user.UUID().String()})
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)

	_, err = repo.Find(cancelled(), user)
	assert.Error(t, err)
	assert.Error(t, repo.Save(cancelled(), user, preferences))
}

func TestPreferencesRepository_ReadsEachValueOnItsOwn(t *testing.T) {
	db := testDB(t)
	repo := NewMongoPreferencesRepository(db)
	ctx := context.Background()
	user := newUser()

	// Written outside the API: what's fine stays, each other value is its default.
	insertRaw(t, db, "preferences", bson.M{"_id": user.String(), "estimate": bson.M{
		"contribution_usd": "lots", "years": 99, "yield_mode": "CUSTOM", "custom_yield_pct": "12",
		"milestones_usd": []string{"500000.00", "x", "100000.00"}, "inflation_pct": "80", "contribution_growth_pct": "2",
	}, "auto_snapshot": "DAILY", "default_view": "estimate", "history_period": "10Y"})
	got, err := repo.Find(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, model.AutoSnapshotOff, got.AutoSnapshot)
	assert.Equal(t, model.StartView("estimate"), got.DefaultView)
	assert.Equal(t, model.HistoryPeriod("1Y"), got.HistoryPeriod)
	defaults := model.DefaultPreferences().Estimate
	assert.Equal(t, defaults.Contribution, got.Estimate.Contribution)
	assert.Equal(t, defaults.Years, got.Estimate.Years)
	assert.Equal(t, model.YieldModeCustom, got.Estimate.YieldMode)
	assert.Equal(t, "12", got.Estimate.CustomYieldPct.String())
	assert.Equal(t, []string{"100000.00", "500000.00"}, []string{got.Estimate.Milestones[0].String(), got.Estimate.Milestones[1].String()})
	assert.True(t, got.Estimate.InflationPct.IsZero())
	assert.Equal(t, "2", got.Estimate.ContributionGrowthPct.String())

	// Without an estimate at all (nor what came after it): the defaults.
	other := newUser()
	insertRaw(t, db, "preferences", bson.M{"_id": other.String()})
	got, err = repo.Find(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, model.DefaultPreferences(), *got)
}

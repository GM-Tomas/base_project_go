package mongo

import (
	"context"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMovementRepository_GroupsAPeriodByShape(t *testing.T) {
	db := testDB(t)
	repo := NewMongoMovementRepository(db)
	ctx := context.Background()
	user, other := newUser(), newUser()
	savings, fund := holding(user, "Savings", "Cash", "Bank", 1000, at(1)), holding(user, "Fund", "Equity", "Broker", 500, at(1))
	visa := model.DebtRef{Id: model.NewDebtId(), Name: "Visa"}
	amount := func(m model.Movement, v float64) model.Movement {
		m.Amount = model.MustMoneyFromFloat(v)
		return m
	}
	ofDebt := func(kind model.MovementKind, v float64, day int, from *model.Holding) model.Movement {
		m := model.Movement{Id: model.NewMovementId(), UserId: user, Kind: kind, OccurredAt: at(day), Amount: model.MustMoneyFromFloat(v),
			Fee: model.ZeroMoney, Debt: &visa, CreatedAt: at(day)}
		if from != nil {
			ref := model.RefOf(*from)
			if kind == model.MovementDebtCharge {
				m.ToHolding = &ref
			} else {
				m.Holding = &ref
			}
		}
		return m
	}
	transfer := amount(movementOf(user, model.MovementTransfer, &savings, at(3)), 200)
	toRef := model.RefOf(fund)
	transfer.ToHolding, transfer.Fee = &toRef, model.MustMoneyFromFloat(2.5)
	fix := movementOf(user, model.MovementAdjustment, &fund, at(4))
	prev, next := model.MustMoneyFromFloat(700), model.MustMoneyFromFloat(680.25)
	fix.Amount, fix.PreviousValue, fix.NewValue = model.MustMoneyFromFloat(19.75), &prev, &next
	debtFix := ofDebt(model.MovementAdjustment, 5, 4, nil)
	owedBefore, owedAfter := model.MustMoneyFromFloat(100), model.MustMoneyFromFloat(105)
	debtFix.PreviousValue, debtFix.NewValue = &owedBefore, &owedAfter

	for _, m := range []model.Movement{
		amount(movementOf(user, model.MovementOpening, &savings, at(1)), 1000),
		amount(movementOf(user, model.MovementGain, &fund, at(2)), 60.1),
		amount(movementOf(user, model.MovementGain, &fund, at(5)), 39.9),
		transfer, fix, debtFix,
		ofDebt(model.MovementDebtPayment, 100, 6, &savings),
		ofDebt(model.MovementDebtPayment, 50, 6, nil),
		ofDebt(model.MovementDebtCharge, 80, 7, nil),
		ofDebt(model.MovementDebtCharge, 300, 7, &savings),
		amount(movementOf(user, model.MovementGain, &fund, at(20)), 1), // after the period
		amount(movementOf(other, model.MovementGain, &fund, at(2)), 999),
	} {
		require.NoError(t, repo.Save(ctx, m))
	}
	// An amount that can't be read (only writable outside the API) is counted, and adds nothing.
	insertRaw(t, db, "movements", bson.M{"_id": uuid.NewString(), "user_id": user.String(), "kind": "GAIN", "occurred_at": at(2),
		"amount_usd": "lots", "holding": bson.M{"id": fund.Id.String()}, "created_at": at(2)})

	groups, err := repo.Groups(ctx, user, at(1), at(10))
	require.NoError(t, err)
	byShape := map[model.MovementGroup]model.MovementGroup{}
	for _, g := range groups {
		byShape[model.MovementGroup{Kind: g.Kind, OfDebt: g.OfDebt, WithHolding: g.WithHolding}] = g
	}
	gains := byShape[model.MovementGroup{Kind: model.MovementGain, WithHolding: true}]
	assert.Equal(t, 3, gains.Count)
	assert.Equal(t, "100", gains.Amount.String())
	tr := byShape[model.MovementGroup{Kind: model.MovementTransfer, WithHolding: true}]
	assert.Equal(t, "200", tr.Amount.String())
	assert.Equal(t, "2.5", tr.Fee.String())
	assert.Equal(t, "-19.75", byShape[model.MovementGroup{Kind: model.MovementAdjustment, WithHolding: true}].Change.String())
	assert.Equal(t, "5", byShape[model.MovementGroup{Kind: model.MovementAdjustment, OfDebt: true}].Change.String())
	assert.Equal(t, "100", byShape[model.MovementGroup{Kind: model.MovementDebtPayment, OfDebt: true, WithHolding: true}].Amount.String())
	assert.Equal(t, "50", byShape[model.MovementGroup{Kind: model.MovementDebtPayment, OfDebt: true}].Amount.String())
	assert.Equal(t, "300", byShape[model.MovementGroup{Kind: model.MovementDebtCharge, OfDebt: true, WithHolding: true}].Amount.String())

	summary := service.MovementsEffect(groups)
	assert.Equal(t, 11, summary.Count)
	assert.Equal(t, 3, summary.Transfers)
	assert.True(t, decimal.RequireFromString("97.5").Equal(summary.Effect.Investments), summary.Effect.Investments.String())
	assert.True(t, decimal.RequireFromString("-30").Equal(summary.Effect.Saving), summary.Effect.Saving.String())
	assert.True(t, decimal.RequireFromString("-24.75").Equal(summary.Effect.Corrections), summary.Effect.Corrections.String())

	// Bounds are inclusive; nothing in range is nothing.
	groups, err = repo.Groups(ctx, user, at(20), at(20))
	require.NoError(t, err)
	require.Len(t, groups, 1)
	groups, err = repo.Groups(ctx, user, at(21), at(30))
	require.NoError(t, err)
	assert.Empty(t, groups)
	_, err = repo.Groups(cancelled(), user, at(1), at(30))
	assert.Error(t, err)
}

func TestSnapshotRepository_KeepsWhereASnapshotCameFrom(t *testing.T) {
	db := testDB(t)
	repo := NewMongoSnapshotRepository(db)
	ctx := context.Background()
	user := newUser()
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	past, err := model.NewManualSnapshot(model.NewSnapshotId(), user, at(1), decimal.NewFromInt(-40), nil, nil, " old ", now)
	require.NoError(t, err)
	_, err = repo.Save(ctx, past)
	require.NoError(t, err)
	_, err = repo.Save(ctx, model.NewNetWorthSnapshot(model.NewSnapshotId(), user, at(2), model.MustMoneyFromFloat(10), model.ZeroMoney))
	require.NoError(t, err)

	all, err := repo.FindAll(ctx, user)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, model.SnapshotManual, all[0].Source)
	assert.Equal(t, "old", all[0].Note)
	assert.Equal(t, "40.00", all[0].Debts.String())
	assert.Equal(t, model.SnapshotAuto, all[1].Source)
	assert.Empty(t, all[1].Note)

	// Today's isn't marked at all, as before there were manual ones.
	var raw bson.M
	require.NoError(t, db.Snapshots.FindOne(ctx, bson.M{"_id": all[1].Id.String()}).Decode(&raw))
	assert.NotContains(t, raw, "source")
	assert.NotContains(t, raw, "note")
}

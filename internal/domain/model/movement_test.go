package model_test

import (
	"strings"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMovementKind(t *testing.T) {
	for _, raw := range []string{"OPENING", "CLOSING", "GAIN", "LOSS", "DEPOSIT", "WITHDRAWAL", "TRANSFER", "ADJUSTMENT"} {
		k, err := model.ParseMovementKind(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, string(k))
	}
	for _, raw := range []string{"", "gain", "REFUND"} {
		_, err := model.ParseMovementKind(raw)
		assert.ErrorIs(t, err, model.ErrUnknownMovementKind, raw)
	}
}

func TestMovementKinds_WhatUsersRecordAndWhatCanBeUndone(t *testing.T) {
	for _, k := range []model.MovementKind{model.MovementGain, model.MovementLoss, model.MovementDeposit, model.MovementWithdrawal, model.MovementTransfer} {
		assert.True(t, k.IsRecordable(), k)
		assert.True(t, k.Revertible(), k)
	}
	for _, k := range []model.MovementKind{model.MovementOpening, model.MovementClosing} {
		assert.False(t, k.IsRecordable(), k)
		assert.False(t, k.Revertible(), k)
	}
	assert.False(t, model.MovementAdjustment.IsRecordable())
	assert.True(t, model.MovementAdjustment.Revertible())
}

func TestMovement_Effect(t *testing.T) {
	from, to := model.NewHoldingId(), model.NewHoldingId()
	ref := func(id model.HoldingId) *model.HoldingRef { return &model.HoldingRef{Id: id} }
	m := func(kind model.MovementKind, amount float64) model.Movement {
		return model.Movement{Kind: kind, Amount: model.MustMoneyFromFloat(amount), Holding: ref(from)}
	}
	change := func(id model.HoldingId, delta string) model.BalanceChange {
		return model.BalanceChange{Holding: id, Delta: decimal.RequireFromString(delta)}
	}

	for kind, want := range map[model.MovementKind]string{
		model.MovementOpening: "10.5", model.MovementGain: "10.5", model.MovementDeposit: "10.5",
		model.MovementClosing: "-10.5", model.MovementLoss: "-10.5", model.MovementWithdrawal: "-10.5",
	} {
		got := m(kind, 10.5).Effect()
		require.Len(t, got, 1, kind)
		assert.True(t, got[0].Delta.Equal(decimal.RequireFromString(want)), "%s: %s", kind, got[0].Delta)
		assert.Equal(t, from, got[0].Holding)
	}

	transfer := m(model.MovementTransfer, 1000)
	transfer.Fee, transfer.ToHolding = model.MustMoneyFromFloat(2.5), ref(to)
	got := transfer.Effect()
	require.Len(t, got, 2)
	assert.Equal(t, change(from, "-1000").Holding, got[0].Holding)
	assert.True(t, got[0].Delta.Equal(decimal.RequireFromString("-1000")))
	assert.Equal(t, to, got[1].Holding)
	assert.True(t, got[1].Delta.Equal(decimal.RequireFromString("997.5")))

	prev, next := model.MustMoneyFromFloat(100), model.MustMoneyFromFloat(80)
	adjustment := m(model.MovementAdjustment, 20)
	adjustment.PreviousValue, adjustment.NewValue = &prev, &next
	got = adjustment.Effect()
	require.Len(t, got, 1)
	assert.True(t, got[0].Delta.Equal(decimal.RequireFromString("-20")))

	assert.Nil(t, model.Movement{Kind: "SOMETHING"}.Effect())
}

func TestPositiveAmountAndTransferFee(t *testing.T) {
	amount, err := model.PositiveAmount(10.005)
	require.NoError(t, err)
	assert.Equal(t, "10.01", amount.String())
	for v, want := range map[float64]error{0: model.ErrNonPositiveAmount, 0.004: model.ErrNonPositiveAmount, -1: model.ErrNegativeMoney} {
		_, err := model.PositiveAmount(v)
		assert.ErrorIs(t, err, want, v)
	}

	fee, err := model.TransferFee(10.01, amount)
	require.NoError(t, err)
	assert.Equal(t, "10.01", fee.String())
	_, err = model.TransferFee(10.02, amount)
	assert.ErrorIs(t, err, model.ErrFeeExceedsAmount)
	_, err = model.TransferFee(-1, amount)
	assert.ErrorIs(t, err, model.ErrNegativeMoney)
}

func TestNormalizeNote(t *testing.T) {
	note, err := model.NormalizeNote("  Dividends, Q3 \n")
	require.NoError(t, err)
	assert.Equal(t, "Dividends, Q3", note)

	note, err = model.NormalizeNote(strings.Repeat("🙂", model.MaxMovementNoteLength))
	require.NoError(t, err, "characters, not bytes")
	assert.Len(t, []rune(note), model.MaxMovementNoteLength)

	_, err = model.NormalizeNote(strings.Repeat("x", model.MaxMovementNoteLength+1))
	assert.ErrorIs(t, err, model.ErrLabelTooLong)
	assert.ErrorContains(t, err, "Note")
}

func TestCheckOccurredAt(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, ok := range []time.Time{now, now.Add(23 * time.Hour), time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)} {
		assert.NoError(t, model.CheckOccurredAt(ok, now), ok)
	}
	for _, bad := range []time.Time{now.Add(25 * time.Hour), time.Date(1969, 12, 31, 23, 59, 59, 0, time.UTC)} {
		assert.ErrorIs(t, model.CheckOccurredAt(bad, now), model.ErrOccurredAtOutOfRange, bad)
	}
}

func TestHolding_WithDelta(t *testing.T) {
	h := model.Holding{Name: "BTC", Value: model.MustMoneyFromFloat(100)}

	up, err := h.WithDelta(decimal.RequireFromString("0.005"))
	require.NoError(t, err)
	assert.Equal(t, "100.01", up.Value.String())

	zero, err := h.WithDelta(decimal.RequireFromString("-100"))
	require.NoError(t, err)
	assert.True(t, zero.Value.IsZero())

	same, err := h.WithDelta(decimal.RequireFromString("-100.01"))
	assert.ErrorIs(t, err, model.ErrNegativeBalance)
	assert.Equal(t, h, same, "unchanged")
}

func TestMoney_USD(t *testing.T) {
	for v, want := range map[float64]string{0: "$0.00", 12.3: "$12.30", 999.99: "$999.99", 1234.56: "$1,234.56", 1e6: "$1,000,000.00", 123456789.1: "$123,456,789.10"} {
		assert.Equal(t, want, model.MustMoneyFromFloat(v).USD())
	}
}

func TestMovementId(t *testing.T) {
	id := model.NewMovementId()
	parsed, err := model.ParseMovementId(id.String())
	require.NoError(t, err)
	assert.Equal(t, id, parsed)
	assert.Equal(t, id, model.MovementIdFromUUID(id.UUID()))
	_, err = model.ParseMovementId("nope")
	assert.ErrorIs(t, err, model.ErrInvalidUUID)
	assert.NotEqual(t, uuid.Nil, id.UUID())
}

func TestRefOf_KeepsWhatTheActivityLogShows(t *testing.T) {
	h := model.Holding{Id: model.NewHoldingId(), Name: "BTC", Platform: model.MustPlatformName("Ledger"), AssetClass: model.MustAssetClass("Crypto"), Value: model.MustMoneyFromFloat(1)}
	assert.Equal(t, model.HoldingRef{Id: h.Id, Name: "BTC", Platform: h.Platform, AssetClass: h.AssetClass}, model.RefOf(h))
}

func TestValueChangeReason(t *testing.T) {
	for raw, want := range map[string]model.ValueChangeReason{"": model.ReasonMarket, "MARKET": model.ReasonMarket, "CASH_FLOW": model.ReasonCashFlow, "CORRECTION": model.ReasonCorrection} {
		got, err := model.ParseValueChangeReason(raw)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := model.ParseValueChangeReason("market")
	assert.ErrorIs(t, err, model.ErrUnknownValueChangeReason)

	low, high := model.MustMoneyFromFloat(10), model.MustMoneyFromFloat(20)
	assert.Equal(t, model.MovementGain, model.ReasonMarket.KindFor(low, high))
	assert.Equal(t, model.MovementLoss, model.ReasonMarket.KindFor(high, low))
	assert.Equal(t, model.MovementDeposit, model.ReasonCashFlow.KindFor(low, high))
	assert.Equal(t, model.MovementWithdrawal, model.ReasonCashFlow.KindFor(high, low))
	assert.Equal(t, model.MovementAdjustment, model.ReasonCorrection.KindFor(low, high))
	assert.Equal(t, model.MovementAdjustment, model.ReasonCorrection.KindFor(high, low))
}

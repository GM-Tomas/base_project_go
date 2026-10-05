package model_test

import (
	"math"
	"strings"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebtId(t *testing.T) {
	u := uuid.New()
	id, err := model.ParseDebtId(u.String())
	require.NoError(t, err)
	assert.Equal(t, u, id.UUID())
	assert.Equal(t, u.String(), id.String())
	assert.Equal(t, id, model.DebtIdFromUUID(u))
	assert.NotEqual(t, model.NewDebtId(), model.NewDebtId())
	_, err = model.ParseDebtId("nope")
	assert.ErrorIs(t, err, model.ErrInvalidUUID)
}

func TestParseDebtKind(t *testing.T) {
	for _, raw := range []string{"CREDIT_CARD", "LOAN", "MORTGAGE", "PERSONAL", "OTHER"} {
		k, err := model.ParseDebtKind(raw)
		require.NoError(t, err)
		assert.Equal(t, raw, string(k))
	}
	k, err := model.ParseDebtKind("")
	require.NoError(t, err)
	assert.Equal(t, model.DebtOther, k, "none is OTHER")
	_, err = model.ParseDebtKind("card")
	assert.ErrorIs(t, err, model.ErrUnknownDebtKind)
	assert.EqualError(t, err, `kind must be one of CREDIT_CARD, LOAN, MORTGAGE, PERSONAL, OTHER (got "card")`)
}

func TestDebtLabels(t *testing.T) {
	name, err := model.NormalizeDebtName("  Visa   Gold ")
	require.NoError(t, err)
	assert.Equal(t, "Visa Gold", name)
	_, err = model.NormalizeDebtName(" ")
	assert.ErrorIs(t, err, model.ErrBlankLabel)
	_, err = model.NormalizeDebtName(strings.Repeat("x", 121))
	assert.EqualError(t, err, "Debt name exceeds max length (121 > 120)")

	lender, err := model.NormalizeDebtLender("   ")
	require.NoError(t, err)
	assert.Empty(t, lender, "a lender is optional")
	lender, err = model.NormalizeDebtLender(" Banco  Galicia ")
	require.NoError(t, err)
	assert.Equal(t, "Banco Galicia", lender)
	_, err = model.NormalizeDebtLender(strings.Repeat("x", 121))
	assert.EqualError(t, err, "Lender exceeds max length (121 > 120)")

	notes, err := model.NormalizeDebtNotes("  0% for 12 months,\nthen 65%  ")
	require.NoError(t, err)
	assert.Equal(t, "0% for 12 months,\nthen 65%", notes, "line breaks are kept")
	_, err = model.NormalizeDebtNotes(strings.Repeat("é", 501))
	assert.EqualError(t, err, "Notes exceeds max length (501 > 500)")
}

func TestDebtTerms(t *testing.T) {
	rate, err := model.NewInterestRatePct(65.555)
	require.NoError(t, err)
	assert.Equal(t, "65.56", rate.StringFixed(2))
	for _, v := range []float64{-0.01, 200.01, math.NaN(), math.Inf(1)} {
		_, err := model.NewInterestRatePct(v)
		assert.ErrorIs(t, err, model.ErrInterestRateOutOfRange, v)
	}
	_, err = model.NewInterestRatePct(200)
	assert.NoError(t, err)

	for _, v := range []int{1, 31} {
		day, err := model.NewDueDay(v)
		require.NoError(t, err)
		assert.Equal(t, v, day)
	}
	for _, v := range []int{0, 32} {
		_, err := model.NewDueDay(v)
		assert.EqualError(t, err, "dueDay must be between 1 and 31")
	}
}

func TestDebt_WithDelta(t *testing.T) {
	d := model.Debt{Balance: model.MustMoneyFromFloat(100)}
	more, err := d.WithDelta(decimal.RequireFromString("25.505"))
	require.NoError(t, err)
	assert.Equal(t, "125.51", more.Balance.String())
	paid, err := d.WithDelta(decimal.RequireFromString("-100"))
	require.NoError(t, err)
	assert.True(t, paid.Balance.IsZero())
	_, err = d.WithDelta(decimal.RequireFromString("-100.01"))
	assert.ErrorIs(t, err, model.ErrNegativeBalance)
	assert.Equal(t, "100.00", d.Balance.String(), "unchanged")
}

func TestDebtRefOf(t *testing.T) {
	d := model.Debt{Id: model.NewDebtId(), Name: "Visa", Lender: "Galicia", Balance: model.MustMoneyFromFloat(1)}
	assert.Equal(t, model.DebtRef{Id: d.Id, Name: "Visa", Lender: "Galicia"}, model.DebtRefOf(d))
}

func TestBalanceChangeReason(t *testing.T) {
	low, high := model.MustMoneyFromFloat(100), model.MustMoneyFromFloat(150)
	for raw, want := range map[string]model.BalanceChangeReason{
		"": model.ReasonDebtCorrection, "CORRECTION": model.ReasonDebtCorrection,
		"PAYMENT": model.ReasonPayment, "CHARGE": model.ReasonCharge, "INTEREST": model.ReasonInterest,
	} {
		got, err := model.ParseBalanceChangeReason(raw)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := model.ParseBalanceChangeReason("REFUND")
	assert.EqualError(t, err, `balanceChangeReason must be one of PAYMENT, CHARGE, INTEREST, CORRECTION (got "REFUND")`)

	kind := func(r model.BalanceChangeReason, previous, next model.Money) (model.MovementKind, error) {
		return r.KindFor(previous, next)
	}
	k, err := kind(model.ReasonPayment, high, low)
	require.NoError(t, err)
	assert.Equal(t, model.MovementDebtPayment, k)
	k, err = kind(model.ReasonCharge, low, high)
	require.NoError(t, err)
	assert.Equal(t, model.MovementDebtCharge, k)
	k, err = kind(model.ReasonInterest, low, high)
	require.NoError(t, err)
	assert.Equal(t, model.MovementDebtInterest, k)
	for _, previous := range []model.Money{low, high} {
		k, err = kind(model.ReasonDebtCorrection, previous, model.MustMoneyFromFloat(120))
		require.NoError(t, err)
		assert.Equal(t, model.MovementAdjustment, k, "a correction goes either way")
	}

	_, err = kind(model.ReasonPayment, low, high)
	assert.ErrorIs(t, err, model.ErrPaymentRaisesBalance)
	_, err = kind(model.ReasonCharge, high, low)
	assert.ErrorIs(t, err, model.ErrChargeLowersBalance)
	_, err = kind(model.ReasonInterest, high, low)
	assert.ErrorIs(t, err, model.ErrChargeLowersBalance)
}

func TestSignedMoney(t *testing.T) {
	net := model.NetOf(model.MustMoneyFromFloat(100), model.MustMoneyFromFloat(250.255))
	assert.Equal(t, "-150.26", net.String())
	assert.Equal(t, -150.26, net.Float64())
	assert.True(t, net.Amount().Equal(decimal.RequireFromString("-150.26")))
	assert.Equal(t, -1, net.Cmp(model.ZeroSignedMoney))
	assert.Equal(t, "100.00", model.MustMoneyFromFloat(100).Signed().String())

	parsed, err := model.ParseSignedMoney("-12.345")
	require.NoError(t, err)
	assert.Equal(t, "-12.35", parsed.String(), "rounded half-up, away from zero")
	_, err = model.ParseSignedMoney("lots")
	assert.Error(t, err)

	// A change has a percentage only from a baseline above zero.
	pct := model.NewSignedMoney(decimal.NewFromInt(50)).GrowthPctFrom(model.NewSignedMoney(decimal.NewFromInt(200)))
	require.NotNil(t, pct)
	assert.Equal(t, "-75.0", pct.StringFixed(1))
	assert.Nil(t, net.GrowthPctFrom(model.ZeroSignedMoney))
	assert.Nil(t, net.GrowthPctFrom(net))
}

func TestMovement_DebtEffect(t *testing.T) {
	debt := &model.DebtRef{Id: model.NewDebtId(), Name: "Visa"}
	m := func(kind model.MovementKind) model.Movement {
		return model.Movement{Kind: kind, Amount: model.MustMoneyFromFloat(300), Debt: debt}
	}
	for kind, want := range map[model.MovementKind]string{
		model.MovementOpening: "300", model.MovementDebtCharge: "300", model.MovementDebtInterest: "300",
		model.MovementClosing: "-300", model.MovementDebtPayment: "-300",
	} {
		delta, ok := m(kind).DebtEffect()
		require.True(t, ok, kind)
		assert.True(t, delta.Equal(decimal.RequireFromString(want)), "%s: %s", kind, delta)
		assert.Nil(t, m(kind).Effect(), "%s touches no holding", kind)
	}
	prev, next := model.MustMoneyFromFloat(1000), model.MustMoneyFromFloat(1250)
	adjustment := m(model.MovementAdjustment)
	adjustment.PreviousValue, adjustment.NewValue = &prev, &next
	delta, ok := adjustment.DebtEffect()
	require.True(t, ok)
	assert.True(t, delta.Equal(decimal.NewFromInt(250)))
	assert.Nil(t, adjustment.Effect())

	// A payment from a holding takes it from there; a charge into a holding adds it there.
	savings := &model.HoldingRef{Id: model.NewHoldingId(), Name: "Savings"}
	payment := m(model.MovementDebtPayment)
	payment.Holding = savings
	require.Len(t, payment.Effect(), 1)
	assert.Equal(t, savings.Id, payment.Effect()[0].Holding)
	assert.True(t, payment.Effect()[0].Delta.Equal(decimal.NewFromInt(-300)))
	charge := m(model.MovementDebtCharge)
	charge.ToHolding = savings
	require.Len(t, charge.Effect(), 1)
	assert.True(t, charge.Effect()[0].Delta.Equal(decimal.NewFromInt(300)))

	_, ok = model.Movement{Kind: model.MovementGain, Amount: model.MustMoneyFromFloat(1)}.DebtEffect()
	assert.False(t, ok, "not about a debt")
	_, ok = model.Movement{Kind: model.MovementTransfer, Debt: debt}.DebtEffect()
	assert.False(t, ok, "a kind that never touches a debt")
}

func TestDebtMovementKinds(t *testing.T) {
	for _, k := range []model.MovementKind{model.MovementDebtPayment, model.MovementDebtCharge, model.MovementDebtInterest} {
		parsed, err := model.ParseMovementKind(string(k))
		require.NoError(t, err)
		assert.Equal(t, k, parsed)
		assert.True(t, k.IsRecordable(), k)
		assert.True(t, k.Revertible(), k)
		assert.True(t, k.IsDebtKind(), k)
	}
	assert.False(t, model.MovementGain.IsDebtKind())
}

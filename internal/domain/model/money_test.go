package model_test

import (
	"math"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoney_RoundsHalfUp(t *testing.T) {
	d, _ := decimal.NewFromString("10.125")
	m, err := model.NewMoney(d)
	require.NoError(t, err)
	assert.Equal(t, "10.13", m.String())
}

func TestMoney_NoPhantomCents(t *testing.T) {
	third := model.MustMoney(decimal.RequireFromString("33.33"))
	total := third.Plus(third).Plus(third)
	assert.Equal(t, "99.99", total.String())
}

func TestMoney_NoBinaryFloatDrift(t *testing.T) {
	m1 := model.MustMoney(decimal.RequireFromString("0.1"))
	m2 := model.MustMoney(decimal.RequireFromString("0.2"))
	sum := m1.Plus(m2)
	assert.Equal(t, "0.30", sum.String())
}

func TestMoney_RejectsNegative(t *testing.T) {
	_, err := model.NewMoney(decimal.RequireFromString("-0.01"))
	assert.Error(t, err)
}

func TestMoney_PercentOfZeroTotalIsNull(t *testing.T) {
	m := model.MustMoney(decimal.RequireFromString("50"))
	pct := m.PercentOf(model.ZeroMoney)
	assert.Nil(t, pct)
}

func TestMoney_PercentOfRoundsToOneDecimal(t *testing.T) {
	m := model.MustMoney(decimal.RequireFromString("33.333"))
	total := model.MustMoney(decimal.RequireFromString("100"))
	pct := m.PercentOf(total)
	require.NotNil(t, pct)
	assert.Equal(t, "33.3", pct.StringFixed(1))
}

func TestMoney_SumOfEmptyIsZero(t *testing.T) {
	assert.True(t, model.SumMoney(nil).IsZero())
	assert.True(t, model.SumMoney([]model.Money{}).IsZero())
}

func TestMoney_MinusNeverGoesBelowZero(t *testing.T) {
	m5 := model.MustMoney(decimal.RequireFromString("5"))
	m10 := model.MustMoney(decimal.RequireFromString("10"))
	diff := m5.Minus(m10)
	assert.Equal(t, model.ZeroMoney, diff)
}

func TestMoney_GrowthPctFromZeroBaselineIsNull(t *testing.T) {
	m := model.MustMoney(decimal.RequireFromString("50"))
	growth := m.GrowthPctFrom(model.ZeroMoney)
	assert.Nil(t, growth)
}

func TestMoney_GrowthPctFromComputesSignedChange(t *testing.T) {
	m1100 := model.MustMoney(decimal.RequireFromString("1100"))
	m900 := model.MustMoney(decimal.RequireFromString("900"))
	baseline := model.MustMoney(decimal.RequireFromString("1000"))

	up := m1100.GrowthPctFrom(baseline)
	require.NotNil(t, up)
	assert.Equal(t, "10.0", up.StringFixed(1))

	down := m900.GrowthPctFrom(baseline)
	require.NotNil(t, down)
	assert.Equal(t, "-10.0", down.StringFixed(1))
}

func TestMoney_FromFloatRejectsNonFinite(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := model.NewMoneyFromFloat(v)
		assert.ErrorIs(t, err, model.ErrNonFiniteMoney)
	}
}

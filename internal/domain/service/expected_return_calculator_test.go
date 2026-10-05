package service_test

import (
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pct(v string) *decimal.Decimal {
	d := decimal.RequireFromString(v)
	return &d
}

func TestExpectedReturn_WeightedByValue(t *testing.T) {
	r := service.CalculateExpectedReturn([]model.HoldingReturn{
		{Value: money(60000), Pct: pct("8")},   // an index fund
		{Value: money(30000), Pct: pct("4.5")}, // a term deposit
		{Value: money(10000)},                  // cash, no return set: 0%
	})

	require.NotNil(t, r.WeightedPct)
	// (60,000 × 8 + 30,000 × 4.5) / 100,000
	assert.Equal(t, "6.15", r.WeightedPct.String())
	assert.Equal(t, "90", r.CoveragePct.String())
	assert.Equal(t, "6150.00", r.Annual.String())
}

func TestExpectedReturn_NegativeAndRounded(t *testing.T) {
	r := service.CalculateExpectedReturn([]model.HoldingReturn{
		{Value: money(1000), Pct: pct("-50")},
		{Value: money(2000), Pct: pct("10")},
	})
	// (−500 + 200) / 3,000 = −10%.
	assert.Equal(t, "-10", r.WeightedPct.String())
	assert.Equal(t, "-300.00", r.Annual.String())
	assert.Equal(t, "100", r.CoveragePct.String())

	r = service.CalculateExpectedReturn([]model.HoldingReturn{{Value: money(3), Pct: pct("1")}, {Value: money(6)}})
	assert.Equal(t, "0.33", r.WeightedPct.String())
	assert.Equal(t, "33.3", r.CoveragePct.String())
	assert.Equal(t, "0.03", r.Annual.String())
}

func TestExpectedReturn_NothingToWeigh(t *testing.T) {
	for _, holdings := range [][]model.HoldingReturn{nil, {{Value: model.ZeroMoney, Pct: pct("7")}}} {
		r := service.CalculateExpectedReturn(holdings)
		assert.Nil(t, r.WeightedPct)
		assert.True(t, r.CoveragePct.IsZero())
		assert.Equal(t, "0.00", r.Annual.String())
	}

	// Holdings without returns: 0%, nothing covered.
	r := service.CalculateExpectedReturn([]model.HoldingReturn{{Value: money(500)}})
	assert.Equal(t, "0", r.WeightedPct.String())
	assert.Equal(t, "0", r.CoveragePct.String())
}

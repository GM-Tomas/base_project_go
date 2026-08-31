package service_test

import (
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func money(v float64) model.Money {
	return model.MustMoneyFromFloat(v)
}

func TestCompoundInterestCalculator_ZeroYield(t *testing.T) {
	fv := service.FutureValue(money(10000.0), money(500.0), decimal.Zero, 60)
	assert.InDelta(t, 40000.0, fv.Float64(), 0.01)
}

func TestCompoundInterestCalculator_PositiveYieldTenYears(t *testing.T) {
	fv := service.FutureValue(money(50000.0), money(1000.0), decimal.RequireFromString("10.0"), 120)
	assert.InDelta(t, 340197.05, fv.Float64(), 0.01)
}

func TestCompoundInterestCalculator_OneYear(t *testing.T) {
	fv := service.FutureValue(money(84250.0), money(900.0), decimal.RequireFromString("9.0"), 12)
	assert.InDelta(t, 103410.06, fv.Float64(), 0.01)
}

func TestCompoundInterestCalculator_FiftyYears(t *testing.T) {
	fv := service.FutureValue(money(10000.0), money(200.0), decimal.RequireFromString("7.0"), 600)
	assert.InDelta(t, 1417418.32, fv.Float64(), 0.01)
}

func TestCompoundInterestCalculator_ZeroEverything(t *testing.T) {
	fv := service.FutureValue(model.ZeroMoney, model.ZeroMoney, decimal.RequireFromString("8.0"), 120)
	assert.True(t, fv.IsZero())
}

func TestCompoundInterestCalculator_MilestoneAlreadyAchieved(t *testing.T) {
	months := service.MonthsToReach(
		money(100.0),
		money(200.0),
		money(0.0),
		decimal.Zero,
		600,
	)
	require.NotNil(t, months)
	assert.Equal(t, 0, *months)
}

func TestCompoundInterestCalculator_MilestoneUnreachable(t *testing.T) {
	months := service.MonthsToReach(
		money(1_000_000.0),
		money(0.0),
		money(0.0),
		decimal.Zero,
		12,
	)
	assert.Nil(t, months)
}

func TestCompoundInterestCalculator_MilestoneReachableWithinAYear(t *testing.T) {
	months := service.MonthsToReach(
		money(100000.0),
		money(80000.0),
		money(2000.0),
		decimal.RequireFromString("8.0"),
		600,
	)
	require.NotNil(t, months)
	assert.Equal(t, 8, *months)
}

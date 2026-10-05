package service_test

import (
	"math"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func money(v float64) model.Money {
	return model.MustMoneyFromFloat(v)
}

// closedForm is the projection's formula before it ran month by month: FV = P(1+r)ⁿ + PMT((1+r)ⁿ − 1)/r.
func closedForm(principal, contribution, yieldPct float64, months int) float64 {
	r := yieldPct / 100 / 12
	if r == 0 {
		return principal + contribution*float64(months)
	}
	compound := math.Pow(1+r, float64(months))
	return principal*compound + contribution*((compound-1)/r)
}

// valueAfter is the portfolio projected years ahead.
func valueAfter(t *testing.T, principal, contribution float64, yieldPct string, years int) model.ProjectionPoint {
	t.Helper()
	params, err := model.NewProjectionParams(money(principal), money(contribution), decimal.RequireFromString(yieldPct), years, nil)
	require.NoError(t, err)
	return service.CalculateSeries(params)[years]
}

func TestSimulation_KnownValues(t *testing.T) {
	assert.InDelta(t, 40000.0, valueAfter(t, 10000, 500, "0", 5).FutureValue.Float64(), 0.001)
	assert.InDelta(t, 340197.05, valueAfter(t, 50000, 1000, "10.0", 10).FutureValue.Float64(), 0.001)
	assert.InDelta(t, 103410.06, valueAfter(t, 84250, 900, "9.0", 1).FutureValue.Float64(), 0.001)
	assert.InDelta(t, 1417418.32, valueAfter(t, 10000, 200, "7.0", 50).FutureValue.Float64(), 0.001)
	assert.True(t, valueAfter(t, 0, 0, "8.0", 10).FutureValue.IsZero())
}

func TestSimulation_MatchesTheClosedFormWithoutRaiseOrInflation(t *testing.T) {
	cases := []struct {
		principal, contribution float64
		yieldPct                string
	}{
		{84250, 900, "9"}, {0, 1000, "5.5"}, {1e6, 0, "12"}, {1234.56, 78.9, "0.01"}, {500000, 2500, "-3"}, {1e9, 1e6, "25"},
	}
	for _, c := range cases {
		params, err := model.NewProjectionParams(money(c.principal), money(c.contribution), decimal.RequireFromString(c.yieldPct), 50, nil)
		require.NoError(t, err)
		yield, _ := decimal.RequireFromString(c.yieldPct).Float64()
		for _, point := range service.CalculateSeries(params) {
			want := closedForm(c.principal, c.contribution, yield, point.Year*12)
			assert.InDelta(t, want, point.FutureValue.Float64(), math.Max(0.01, want*1e-12), "%v year %d", c, point.Year)
			assert.Equal(t, point.FutureValue.String(), point.RealFutureValue.String(), "no inflation: today's dollars are the same")
		}
	}
}

func TestSimulation_RaisesTheContributionEachYear(t *testing.T) {
	params, err := model.NewProjectionParams(model.ZeroMoney, money(100), decimal.Zero, 3, nil)
	require.NoError(t, err)
	params, err = params.WithAdjustments(decimal.Zero, decimal.NewFromInt(10))
	require.NoError(t, err)

	series := service.CalculateSeries(params)
	// 12 × 100, then 12 × 110, then 12 × 121.
	assert.Equal(t, "1200.00", series[1].TotalContributed.String())
	assert.Equal(t, "2520.00", series[2].TotalContributed.String())
	assert.Equal(t, "3972.00", series[3].TotalContributed.String())
	assert.Equal(t, "3972.00", series[3].FutureValue.String())
	assert.Equal(t, "0.00", series[3].InterestEarned.String())
}

func TestSimulation_DeflatesIntoTodaysDollars(t *testing.T) {
	params, err := model.NewProjectionParams(money(100000), model.ZeroMoney, decimal.Zero, 2, nil)
	require.NoError(t, err)
	params, err = params.WithAdjustments(decimal.NewFromInt(12), decimal.Zero)
	require.NoError(t, err)
	params.DebtBalances = service.DebtBalances(model.Debt{Balance: money(1000)}, 24)

	series := service.CalculateSeries(params)
	assert.Equal(t, "100000.00", series[0].RealFutureValue.String())
	// 1% a month for 12 months: 100,000 / 1.01¹².
	assert.Equal(t, "100000.00", series[1].FutureValue.String())
	assert.Equal(t, "88744.92", series[1].RealFutureValue.String())
	assert.Equal(t, "87857.47", series[1].RealNetWorth.String())
	assert.Equal(t, "99000.00", series[1].NetWorth.String(), "the nominal values don't change")
}

func TestSimulation_NegativeYieldsLose(t *testing.T) {
	last := valueAfter(t, 10000, 0, "-12", 1)
	assert.InDelta(t, 10000*math.Pow(0.99, 12), last.FutureValue.Float64(), 0.01)
	assert.True(t, last.InterestEarned.Amount().IsNegative())
	// A total loss leaves nothing, never less.
	assert.Equal(t, "0.00", valueAfter(t, 10000, 0, "-100", 50).FutureValue.String())
}

func TestMilestones_AlreadyThereUnreachableAndWithinAYear(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	milestone := func(principal, contribution float64, yieldPct string, years int, amount float64) model.Milestone {
		t.Helper()
		params, err := model.NewProjectionParams(money(principal), money(contribution), decimal.RequireFromString(yieldPct), years,
			[]model.Money{money(amount)})
		require.NoError(t, err)
		return service.CalculateMilestones(params, now)[0]
	}

	there := milestone(200, 0, "0", 50, 100)
	assert.Equal(t, model.MilestoneStatusAchieved, there.Status)
	assert.Equal(t, 0, *there.MonthsRequired)

	assert.Equal(t, model.MilestoneStatusOutOfHorizon, milestone(0, 0, "0", 1, 1_000_000).Status)

	soon := milestone(80000, 2000, "8.0", 50, 100000)
	assert.Equal(t, model.MilestoneStatusReachable, soon.Status)
	assert.Equal(t, 8, *soon.MonthsRequired)
	assert.Equal(t, "2027-04", *soon.TargetMonth)
}

func TestMilestones_SoonerWithARaise(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	params, err := model.NewProjectionParams(model.ZeroMoney, money(1000), decimal.Zero, 10, []model.Money{money(30000)})
	require.NoError(t, err)
	assert.Equal(t, 30, *service.CalculateMilestones(params, now)[0].MonthsRequired)

	params, err = params.WithAdjustments(decimal.Zero, decimal.NewFromInt(50))
	require.NoError(t, err)
	// 12,000 in the first year, then 1,500 a month: 18 more months make 39,000; 12 make 30,000.
	assert.Equal(t, 24, *service.CalculateMilestones(params, now)[0].MonthsRequired)
}

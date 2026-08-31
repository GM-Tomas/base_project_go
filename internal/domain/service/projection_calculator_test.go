package service_test

import (
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectionCalculator_SeriesHasYearsPlusOnePoints(t *testing.T) {
	params, err := model.NewProjectionParams(
		model.MustMoneyFromFloat(84250.0),
		model.MustMoneyFromFloat(900.0),
		decimal.RequireFromString("9.0"),
		12,
		nil,
	)
	require.NoError(t, err)

	series := service.CalculateSeries(params)

	assert.Len(t, series, 13)
	assert.Equal(t, 0, series[0].Year)
	assert.Equal(t, "84250.00", series[0].FutureValue.String())
	assert.Equal(t, "84250.00", series[0].TotalContributed.String())
	assert.Equal(t, "0.00", series[0].InterestEarned.String())
	assert.Equal(t, 12, series[12].Year)
}

func TestProjectionCalculator_MilestoneAlreadyAchieved(t *testing.T) {
	params, err := model.NewProjectionParams(
		model.MustMoneyFromFloat(200000.0),
		model.MustMoneyFromFloat(0.0),
		decimal.Zero,
		5,
		[]model.Money{model.MustMoneyFromFloat(150000.0)},
	)
	require.NoError(t, err)

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	milestones := service.CalculateMilestones(params, now)

	require.Len(t, milestones, 1)
	m := milestones[0]
	assert.Equal(t, model.MilestoneStatusAchieved, m.Status)
	require.NotNil(t, m.MonthsRequired)
	assert.Equal(t, 0, *m.MonthsRequired)
	assert.Nil(t, m.TargetMonth)
}

func TestProjectionCalculator_MilestoneReachableWithinHorizon(t *testing.T) {
	params, err := model.NewProjectionParams(
		model.MustMoneyFromFloat(84250.0),
		model.MustMoneyFromFloat(900.0),
		decimal.RequireFromString("9.0"),
		12,
		[]model.Money{model.MustMoneyFromFloat(150000.0)},
	)
	require.NoError(t, err)

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	milestones := service.CalculateMilestones(params, now)

	require.Len(t, milestones, 1)
	m := milestones[0]
	assert.Equal(t, model.MilestoneStatusReachable, m.Status)
	require.NotNil(t, m.MonthsRequired)
	assert.Equal(t, 38, *m.MonthsRequired)
	require.NotNil(t, m.TargetMonth)
	assert.Equal(t, "2029-10", *m.TargetMonth)
}

func TestProjectionCalculator_MilestoneOutOfHorizon(t *testing.T) {
	params, err := model.NewProjectionParams(
		model.MustMoneyFromFloat(1000.0),
		model.MustMoneyFromFloat(10.0),
		decimal.RequireFromString("1.0"),
		1,
		[]model.Money{model.MustMoneyFromFloat(1_000_000.0)},
	)
	require.NoError(t, err)

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	milestones := service.CalculateMilestones(params, now)

	require.Len(t, milestones, 1)
	m := milestones[0]
	assert.Equal(t, model.MilestoneStatusOutOfHorizon, m.Status)
	assert.Nil(t, m.MonthsRequired)
	assert.Nil(t, m.TargetMonth)
}

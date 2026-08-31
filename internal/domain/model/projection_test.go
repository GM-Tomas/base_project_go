package model_test

import (
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeParams(years int, yieldPct string, milestones []model.Money) (model.ProjectionParams, error) {
	return model.NewProjectionParams(
		model.MustMoneyFromFloat(1000.0),
		model.MustMoneyFromFloat(100.0),
		decimal.RequireFromString(yieldPct),
		years,
		milestones,
	)
}

func TestProjectionParams_RejectsYearsOutOfRange(t *testing.T) {
	_, err0 := makeParams(0, "8.0", nil)
	assert.Error(t, err0)

	_, err51 := makeParams(51, "8.0", nil)
	assert.Error(t, err51)
}

func TestProjectionParams_RejectsYieldOutOfRange(t *testing.T) {
	_, errNeg := makeParams(10, "-0.01", nil)
	assert.Error(t, errNeg)

	_, errOver := makeParams(10, "100.01", nil)
	assert.Error(t, errOver)
}

func TestProjectionParams_RejectsTooManyMilestones(t *testing.T) {
	sixMilestones := []model.Money{
		model.MustMoneyFromFloat(1000),
		model.MustMoneyFromFloat(2000),
		model.MustMoneyFromFloat(3000),
		model.MustMoneyFromFloat(4000),
		model.MustMoneyFromFloat(5000),
		model.MustMoneyFromFloat(6000),
	}
	_, err := makeParams(10, "8.0", sixMilestones)
	assert.Error(t, err)
}

func TestProjectionParams_SortsMilestones(t *testing.T) {
	milestones := []model.Money{
		model.MustMoneyFromFloat(500.0),
		model.MustMoneyFromFloat(100.0),
		model.MustMoneyFromFloat(300.0),
	}
	params, err := makeParams(10, "8.0", milestones)
	require.NoError(t, err)

	assert.Equal(t, "100.00", params.Milestones[0].String())
	assert.Equal(t, "300.00", params.Milestones[1].String())
	assert.Equal(t, "500.00", params.Milestones[2].String())
}

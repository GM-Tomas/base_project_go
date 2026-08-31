package service_test

import (
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func snapshotOf(value float64, at time.Time) model.NetWorthSnapshot {
	return model.NewNetWorthSnapshot(
		model.NewSnapshotId(),
		model.NewUserId(uuid.New()),
		at,
		model.MustMoneyFromFloat(value),
	)
}

func TestYtdGrowthCalculator_PrefersYearStartBaseline(t *testing.T) {
	yearStart := snapshotOf(1000.0, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	earliest := snapshotOf(500.0, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	result := service.CalculateYtdGrowth(model.MustMoneyFromFloat(1100.0), &yearStart, &earliest)

	assert.Equal(t, model.YtdBasisYearStartSnapshot, result.Basis)
	require.NotNil(t, result.BaselineValue)
	assert.Equal(t, "1000.00", result.BaselineValue.String())
	assert.Equal(t, "10.0", result.GrowthPct.StringFixed(1))
}

func TestYtdGrowthCalculator_FallsBackToEarliest(t *testing.T) {
	earliest := snapshotOf(500.0, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	result := service.CalculateYtdGrowth(model.MustMoneyFromFloat(600.0), nil, &earliest)

	assert.Equal(t, model.YtdBasisEarliestSnapshot, result.Basis)
	require.NotNil(t, result.BaselineValue)
	assert.Equal(t, "500.00", result.BaselineValue.String())
	assert.Equal(t, "20.0", result.GrowthPct.StringFixed(1))
}

func TestYtdGrowthCalculator_NoBaselineWhenNoHistory(t *testing.T) {
	result := service.CalculateYtdGrowth(model.MustMoneyFromFloat(600.0), nil, nil)
	assert.Equal(t, model.YtdBasisNoBaseline, result.Basis)
	assert.True(t, result.GrowthPct.IsZero())
}

func TestYtdGrowthCalculator_NoBaselineWhenBaselineIsZero(t *testing.T) {
	zeroBaseline := snapshotOf(0.0, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	result := service.CalculateYtdGrowth(model.MustMoneyFromFloat(600.0), &zeroBaseline, nil)
	assert.Equal(t, model.YtdBasisNoBaseline, result.Basis)
	assert.True(t, result.GrowthPct.IsZero())
}

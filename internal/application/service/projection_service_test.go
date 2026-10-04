package service_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectionService_Project(t *testing.T) {
	wealthAgg := &mockWealthAggregationPort{
		netWorth: model.MustMoneyFromFloat(84250.0),
	}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewProjectionService(wealthAgg, fixedClock(now))

	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	result, err := svc.Project(ctx, inbound.ProjectionRequest{
		UserId:              userId,
		MonthlyContribution: 900.0,
		AnnualYieldPct:      9.0,
		Years:               12,
		Milestones:          []float64{150000.0, 250000.0},
	})
	require.NoError(t, err)

	assert.Equal(t, "84250.00", result.Principal.String())
	assert.Equal(t, 12, result.Years)
	assert.Len(t, result.Series, 13)
	assert.Len(t, result.Milestones, 2)
	assert.Equal(t, model.MilestoneStatusReachable, result.Milestones[0].Status)
	assert.Equal(t, model.MilestoneStatusReachable, result.Milestones[1].Status)
}

func TestProjectionService_RejectsNonFiniteInputsInsteadOfPanicking(t *testing.T) {
	svc := service.NewProjectionService(&mockWealthAggregationPort{netWorth: model.ZeroMoney}, fixedClock(time.Now()))
	base := inbound.ProjectionRequest{UserId: model.NewUserId(uuid.New()), MonthlyContribution: 100, AnnualYieldPct: 5, Years: 1}

	nan := base
	nan.AnnualYieldPct = math.NaN()
	_, err := svc.Project(context.Background(), nan)
	assert.ErrorIs(t, err, model.ErrYieldOutOfRange)

	inf := base
	inf.MonthlyContribution = math.Inf(1)
	_, err = svc.Project(context.Background(), inf)
	assert.ErrorIs(t, err, model.ErrNonFiniteMoney)
}

package service_test

import (
	"context"
	"errors"
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
		assets: model.MustMoneyFromFloat(84250.0),
	}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewProjectionService(wealthAgg, newMockDebtRepo(), fixedClock(now))

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
	svc := service.NewProjectionService(&mockWealthAggregationPort{assets: model.ZeroMoney}, newMockDebtRepo(), fixedClock(time.Now()))
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

func TestProjectionService_ProjectsThePortfolioAndPaysOffDebts(t *testing.T) {
	debts := newMockDebtRepo()
	user := model.NewUserId(uuid.New())
	payment := model.MustMoneyFromFloat(1000)
	debts.debts["visa"] = model.Debt{Id: model.NewDebtId(), UserId: user, Name: "Visa", Balance: model.MustMoneyFromFloat(6000), MonthlyPayment: &payment}
	debts.debts["theirs"] = model.Debt{Id: model.NewDebtId(), UserId: model.NewUserId(uuid.New()), Balance: model.MustMoneyFromFloat(1e6)}
	svc := service.NewProjectionService(&mockWealthAggregationPort{assets: model.MustMoneyFromFloat(10000)}, debts, fixedClock(time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)))

	result, err := svc.Project(context.Background(), inbound.ProjectionRequest{UserId: user, Years: 1, Milestones: []float64{9000}})
	require.NoError(t, err)
	assert.Equal(t, "10000.00", result.Principal.String(), "the portfolio, not the net worth")
	assert.Equal(t, "6000.00", result.Debts.String(), "only the user's debts")
	assert.Equal(t, "4000.00", result.Series[0].NetWorth.String())
	assert.Equal(t, "0.00", result.Series[1].DebtBalance.String(), "paid off in 6 months")
	assert.Equal(t, "10000.00", result.Series[1].NetWorth.String())
	assert.Equal(t, 5, *result.Milestones[0].MonthsRequired, "the net worth reaches 9,000 once 5,000 is paid off")

	debts.findAllErr = errors.New("boom")
	_, err = svc.Project(context.Background(), inbound.ProjectionRequest{UserId: user, Years: 1})
	assert.EqualError(t, err, "boom")

	// Invalid parameters are refused before anything is read.
	_, err = svc.Project(context.Background(), inbound.ProjectionRequest{UserId: user, Years: 0})
	assert.ErrorIs(t, err, model.ErrYearsOutOfRange)
}

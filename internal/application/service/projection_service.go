package service

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	domainService "github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/GM-Tomas/base_project_go/internal/parallel"
)

// ProjectionService projects the portfolio (the assets) with compound interest and monthly contributions.
// Debts aren't part of it: each is paid off on its own terms, and the net worth is what's left after them.
type ProjectionService struct {
	wealthAggregationPort outbound.WealthAggregationPort
	debtRepo              outbound.DebtRepository
	clock                 Clock
}

func NewProjectionService(
	wealthAggregationPort outbound.WealthAggregationPort,
	debtRepo outbound.DebtRepository,
	clock Clock,
) *ProjectionService {
	if clock == nil {
		clock = RealClock
	}
	return &ProjectionService{
		wealthAggregationPort: wealthAggregationPort,
		debtRepo:              debtRepo,
		clock:                 clock,
	}
}

var _ inbound.ProjectionUseCase = (*ProjectionService)(nil)

func (s *ProjectionService) Project(
	ctx context.Context,
	request inbound.ProjectionRequest,
) (inbound.ProjectionResult, error) {
	monthlyContribution, err := model.NewMoneyFromFloat(request.MonthlyContribution)
	if err != nil {
		return inbound.ProjectionResult{}, err
	}

	annualYieldPct, err := model.YieldPctFromFloat(request.AnnualYieldPct)
	if err != nil {
		return inbound.ProjectionResult{}, err
	}

	milestones := make([]model.Money, len(request.Milestones))
	for i, m := range request.Milestones {
		mVal, err := model.NewMoneyFromFloat(m)
		if err != nil {
			return inbound.ProjectionResult{}, err
		}
		milestones[i] = mVal
	}

	params, err := model.NewProjectionParams(
		model.ZeroMoney,
		monthlyContribution,
		annualYieldPct,
		request.Years,
		milestones,
	)
	if err != nil {
		return inbound.ProjectionResult{}, err
	}

	var (
		totals outbound.WealthTotals
		debts  []model.Debt
	)
	err = parallel.Run(ctx,
		func(ctx context.Context) (err error) {
			totals, err = s.wealthAggregationPort.Totals(ctx, request.UserId)
			return err
		},
		func(ctx context.Context) (err error) {
			debts, err = s.debtRepo.FindAll(ctx, request.UserId)
			return err
		},
	)
	if err != nil {
		return inbound.ProjectionResult{}, err
	}
	params.Principal = totals.Assets
	params.DebtBalances = domainService.TotalDebtBalances(debts, params.Years*12)
	owed := model.ZeroMoney
	for _, d := range debts {
		owed = owed.Plus(d.Balance)
	}

	now := s.clock()

	return inbound.ProjectionResult{
		Principal:           params.Principal,
		Debts:               owed,
		MonthlyContribution: params.MonthlyContribution,
		AnnualYieldPct:      params.AnnualYieldPct,
		Years:               params.Years,
		Series:              domainService.CalculateSeries(params),
		Milestones:          domainService.CalculateMilestones(params, now),
	}, nil
}

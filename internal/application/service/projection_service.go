package service

import (
	"context"
	"math"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	domainService "github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/GM-Tomas/base_project_go/internal/parallel"
	"github.com/shopspring/decimal"
)

// ProjectionService projects the portfolio (the assets) with compound interest and monthly contributions, at
// a growth of the user's or the portfolio's expected return. Debts aren't part of it: each is paid off on
// its own terms, and the net worth is what's left after them.
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

	// Without a yield of its own, the projection grows at the portfolio's expected return, known once read.
	annualYieldPct, source := decimal.Zero, model.YieldFromPortfolio
	if request.AnnualYieldPct != nil {
		if annualYieldPct, err = model.YieldPctFromFloat(*request.AnnualYieldPct); err != nil {
			return inbound.ProjectionResult{}, err
		}
		source = model.YieldCustom
	}
	inflationPct, err := adjustmentPct(request.InflationPct, model.ErrInflationOutOfRange)
	if err != nil {
		return inbound.ProjectionResult{}, err
	}
	contributionGrowthPct, err := adjustmentPct(request.ContributionGrowthPct, model.ErrContributionGrowthOutOfRange)
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
	if params, err = params.WithAdjustments(inflationPct, contributionGrowthPct); err != nil {
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
	portfolio := domainService.CalculateExpectedReturn(totals.Returns).WeightedPct
	if source == model.YieldFromPortfolio && portfolio != nil {
		params.AnnualYieldPct = *portfolio
	}
	owed := model.ZeroMoney
	for _, d := range debts {
		owed = owed.Plus(d.Balance)
	}

	now := s.clock()

	return inbound.ProjectionResult{
		Principal:             params.Principal,
		Debts:                 owed,
		MonthlyContribution:   params.MonthlyContribution,
		AnnualYieldPct:        params.AnnualYieldPct,
		YieldSource:           source,
		PortfolioYieldPct:     portfolio,
		InflationPct:          params.InflationPct,
		ContributionGrowthPct: params.ContributionGrowthPct,
		Years:                 params.Years,
		Series:                domainService.CalculateSeries(params),
		Milestones:            domainService.CalculateMilestones(params, now),
	}, nil
}

// adjustmentPct is an inflation or a raise as sent (a finite percentage); its range is the params' to check.
func adjustmentPct(v float64, outOfRange error) (decimal.Decimal, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return decimal.Zero, outOfRange
	}
	return decimal.NewFromFloat(v), nil
}

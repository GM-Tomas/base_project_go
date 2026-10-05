package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

type ProjectionRequest struct {
	UserId              model.UserId
	MonthlyContribution float64
	// AnnualYieldPct is the growth to project with; nil is the portfolio's expected return (0 without one).
	AnnualYieldPct *float64
	Years          int
	Milestones     []float64
	// InflationPct deflates the series into today's dollars; ContributionGrowthPct raises the monthly
	// contribution every 12 months. 0 leaves either out.
	InflationPct          float64
	ContributionGrowthPct float64
}

type ProjectionResult struct {
	// Principal is the portfolio (the assets): the projection grows it. Debts are what's owed now, paid off
	// on their own terms in the series' DebtBalance.
	Principal           model.Money
	Debts               model.Money
	MonthlyContribution model.Money
	AnnualYieldPct      decimal.Decimal
	// YieldSource says where AnnualYieldPct came from; PortfolioYieldPct is the portfolio's expected return
	// (nil with nothing to weigh), whichever was used.
	YieldSource           model.YieldSource
	PortfolioYieldPct     *decimal.Decimal
	InflationPct          decimal.Decimal
	ContributionGrowthPct decimal.Decimal
	Years                 int
	Series                []model.ProjectionPoint
	Milestones            []model.Milestone
}

type ProjectionUseCase interface {
	Project(ctx context.Context, request ProjectionRequest) (ProjectionResult, error)
}

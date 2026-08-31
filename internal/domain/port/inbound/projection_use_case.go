package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

type ProjectionRequest struct {
	UserId              model.UserId
	MonthlyContribution float64
	AnnualYieldPct      float64
	Years               int
	Milestones          []float64
	PrincipalOverride   *float64
}

type ProjectionResult struct {
	Principal           model.Money
	MonthlyContribution model.Money
	AnnualYieldPct      decimal.Decimal
	Years               int
	Series              []model.ProjectionPoint
	Milestones          []model.Milestone
}

type ProjectionUseCase interface {
	Project(ctx context.Context, request ProjectionRequest) (ProjectionResult, error)
}

package service

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	domainService "github.com/GM-Tomas/base_project_go/internal/domain/service"
)

type ProjectionService struct {
	wealthAggregationPort outbound.WealthAggregationPort
	clock                 Clock
}

func NewProjectionService(
	wealthAggregationPort outbound.WealthAggregationPort,
	clock Clock,
) *ProjectionService {
	if clock == nil {
		clock = RealClock
	}
	return &ProjectionService{
		wealthAggregationPort: wealthAggregationPort,
		clock:                 clock,
	}
}

var _ inbound.ProjectionUseCase = (*ProjectionService)(nil)

func (s *ProjectionService) Project(
	ctx context.Context,
	request inbound.ProjectionRequest,
) (inbound.ProjectionResult, error) {
	principal, err := s.wealthAggregationPort.NetWorth(ctx, request.UserId)
	if err != nil {
		return inbound.ProjectionResult{}, err
	}

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
		principal,
		monthlyContribution,
		annualYieldPct,
		request.Years,
		milestones,
	)
	if err != nil {
		return inbound.ProjectionResult{}, err
	}

	now := s.clock()

	return inbound.ProjectionResult{
		Principal:           params.Principal,
		MonthlyContribution: params.MonthlyContribution,
		AnnualYieldPct:      params.AnnualYieldPct,
		Years:               params.Years,
		Series:              domainService.CalculateSeries(params),
		Milestones:          domainService.CalculateMilestones(params, now),
	}, nil
}

package service

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	domainService "github.com/GM-Tomas/base_project_go/internal/domain/service"
)

var DefaultLiquidAssetClasses = []string{
	"Cash",
	"Equity",
	"Crypto",
	"Index Fund",
}

type WealthQueryService struct {
	wealthAggregationPort outbound.WealthAggregationPort
	snapshotRepo          outbound.SnapshotRepository
	clock                 Clock
	liquidAssetClasses    []string
}

func NewWealthQueryService(
	wealthAggregationPort outbound.WealthAggregationPort,
	snapshotRepo outbound.SnapshotRepository,
	clock Clock,
	liquidAssetClasses []string,
) *WealthQueryService {
	if clock == nil {
		clock = RealClock
	}
	if len(liquidAssetClasses) == 0 {
		liquidAssetClasses = DefaultLiquidAssetClasses
	}
	return &WealthQueryService{
		wealthAggregationPort: wealthAggregationPort,
		snapshotRepo:          snapshotRepo,
		clock:                 clock,
		liquidAssetClasses:    liquidAssetClasses,
	}
}

var _ inbound.WealthUseCase = (*WealthQueryService)(nil)

func (s *WealthQueryService) GetSummary(
	ctx context.Context,
	userId model.UserId,
) (dto.WealthSummaryResponse, error) {
	netWorth, err := s.wealthAggregationPort.NetWorth(ctx, userId)
	if err != nil {
		return dto.WealthSummaryResponse{}, err
	}

	byClass, err := s.wealthAggregationPort.ByAssetClass(ctx, userId)
	if err != nil {
		return dto.WealthSummaryResponse{}, err
	}

	byPlatform, err := s.wealthAggregationPort.ByPlatform(ctx, userId)
	if err != nil {
		return dto.WealthSummaryResponse{}, err
	}

	currentYear := s.clock().Year()

	firstOfYear, err := s.snapshotRepo.FindFirstOfYear(ctx, userId, currentYear)
	if err != nil {
		return dto.WealthSummaryResponse{}, err
	}

	earliest, err := s.snapshotRepo.FindEarliest(ctx, userId)
	if err != nil {
		return dto.WealthSummaryResponse{}, err
	}

	ytd := domainService.CalculateYtdGrowth(netWorth, firstOfYear, earliest)

	liquidModelClasses := make([]model.AssetClass, len(s.liquidAssetClasses))
	for i, name := range s.liquidAssetClasses {
		liquidModelClasses[i] = model.MustAssetClass(name)
	}
	policy := domainService.NewLiquidityPolicy(liquidModelClasses)

	valueByClassMap := make(map[string]model.Money, len(byClass))
	totalCount := 0
	for _, agg := range byClass {
		valueByClassMap[agg.AssetClass.Value()] = agg.Value
		totalCount += agg.Count
	}
	liquidityBreakdown := policy.Breakdown(valueByClassMap)

	// YtdDTO
	ytdDTO := dto.YtdDTO{
		Basis:     string(ytd.Basis),
		GrowthPct: ytd.GrowthPct.InexactFloat64(),
	}
	if ytd.BaselineValue != nil {
		val := ytd.BaselineValue.Float64()
		ytdDTO.BaselineValueUsd = &val
	}
	if ytd.BaselineAt != nil {
		ytdDTO.BaselineAt = ytd.BaselineAt
	}

	// LiquidityDTO
	liquidPctF, _ := liquidityBreakdown.LiquidPct.Float64()
	illiquidPctF, _ := liquidityBreakdown.IlliquidPct.Float64()
	liquidityDTO := dto.LiquidityDTO{
		LiquidPct:          liquidPctF,
		IlliquidPct:        illiquidPctF,
		LiquidAssetClasses: s.liquidAssetClasses,
	}

	// ByAssetClass
	byAssetClassDTOs := make([]dto.AssetClassBreakdown, len(byClass))
	for i, agg := range byClass {
		pct := agg.Value.PercentOf(netWorth)
		var pctF float64
		if pct != nil {
			pctF, _ = pct.Round(1).Float64()
		}
		byAssetClassDTOs[i] = dto.AssetClassBreakdown{
			AssetClass: agg.AssetClass.Value(),
			ValueUsd:   agg.Value.Float64(),
			Pct:        pctF,
			Count:      agg.Count,
		}
	}

	// ByPlatform
	byPlatformDTOs := make([]dto.PlatformBreakdown, len(byPlatform))
	for i, agg := range byPlatform {
		pct := agg.Value.PercentOf(netWorth)
		var pctF float64
		if pct != nil {
			pctF, _ = pct.Round(1).Float64()
		}
		byPlatformDTOs[i] = dto.PlatformBreakdown{
			Name:     agg.Name.Value(),
			Type:     agg.Type.Value(),
			ValueUsd: agg.Value.Float64(),
			Pct:      pctF,
			Count:    agg.Count,
		}
	}

	return dto.WealthSummaryResponse{
		NetWorth: dto.NetWorthDTO{
			Usd: netWorth.Float64(),
		},
		HoldingsCount: totalCount,
		Ytd:           ytdDTO,
		Liquidity:     liquidityDTO,
		ByAssetClass:  byAssetClassDTOs,
		ByPlatform:    byPlatformDTOs,
	}, nil
}

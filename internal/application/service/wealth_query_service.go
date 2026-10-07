package service

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	domainService "github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/GM-Tomas/base_project_go/internal/parallel"
)

type WealthQueryService struct {
	wealthAggregationPort outbound.WealthAggregationPort
	snapshotRepo          outbound.SnapshotRepository
	classSettings         outbound.AssetClassSettingsRepository
	platformSettings      outbound.PlatformSettingsRepository
	clock                 Clock
	classDefaults         model.ClassDefaults
}

func NewWealthQueryService(
	wealthAggregationPort outbound.WealthAggregationPort,
	snapshotRepo outbound.SnapshotRepository,
	classSettings outbound.AssetClassSettingsRepository,
	platformSettings outbound.PlatformSettingsRepository,
	clock Clock,
	classDefaults model.ClassDefaults,
) *WealthQueryService {
	if clock == nil {
		clock = RealClock
	}
	return &WealthQueryService{
		wealthAggregationPort: wealthAggregationPort,
		snapshotRepo:          snapshotRepo,
		classSettings:         classSettings,
		platformSettings:      platformSettings,
		clock:                 clock,
		classDefaults:         classDefaults,
	}
}

var _ inbound.WealthUseCase = (*WealthQueryService)(nil)

func (s *WealthQueryService) GetSummary(
	ctx context.Context,
	userId model.UserId,
) (dto.WealthSummaryResponse, error) {
	// The holdings and debts (one read gives the totals and both breakdowns), the two possible YTD baselines
	// and how the user set up their classes and platforms are independent: read all at once, so the summary
	// waits for one round trip rather than one per read.
	currentYear := s.clock().Year()
	var (
		breakdown             outbound.WealthBreakdown
		firstOfYear, earliest *model.NetWorthSnapshot
		classSettings         []model.AssetClassSettings
		platformSettings      []model.PlatformSettings
	)
	err := parallel.Run(ctx,
		func(ctx context.Context) (err error) {
			classSettings, err = s.classSettings.FindAll(ctx, userId)
			return err
		},
		func(ctx context.Context) (err error) {
			platformSettings, err = s.platformSettings.FindAll(ctx, userId)
			return err
		},
		func(ctx context.Context) (err error) {
			breakdown, err = s.wealthAggregationPort.Breakdown(ctx, userId)
			return err
		},
		func(ctx context.Context) (err error) {
			firstOfYear, err = s.snapshotRepo.FindFirstOfYear(ctx, userId, currentYear)
			return err
		},
		func(ctx context.Context) (err error) {
			earliest, err = s.snapshotRepo.FindEarliest(ctx, userId)
			return err
		},
	)
	if err != nil {
		return dto.WealthSummaryResponse{}, err
	}
	assets, debts, byClass, byPlatform := breakdown.Assets, breakdown.Debts, breakdown.ByAssetClass, breakdown.ByPlatform
	netWorth := model.NetOf(assets, debts.Balance)

	ytd := domainService.CalculateYtdGrowth(netWorth, firstOfYear, earliest)

	// Each class counts as ready to spend as the user set it (or as by default).
	classes := model.NewClasses(s.classDefaults, classSettings)
	inUse := make([]model.AssetClass, len(byClass))
	valueByClassMap := make(map[string]model.Money, len(byClass))
	totalCount := 0
	for i, agg := range byClass {
		inUse[i] = agg.AssetClass
		valueByClassMap[agg.AssetClass.Value()] = agg.Value
		totalCount += agg.Count
	}
	var liquidClasses []model.AssetClass
	liquidNames := []string{}
	for _, class := range classes.Visible(inUse) {
		if classes.Liquid(class) {
			liquidClasses = append(liquidClasses, class)
			liquidNames = append(liquidNames, class.Value())
		}
	}
	liquidityBreakdown := domainService.NewLiquidityPolicy(liquidClasses).Breakdown(valueByClassMap)

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
		LiquidAssetClasses: liquidNames,
	}

	// ByAssetClass and ByPlatform: shares of what's owned (the net worth can be zero or below).
	byAssetClassDTOs := make([]dto.AssetClassBreakdown, len(byClass))
	for i, agg := range byClass {
		pct := agg.Value.PercentOf(assets)
		var pctF float64
		if pct != nil {
			pctF, _ = pct.Round(1).Float64()
		}
		byAssetClassDTOs[i] = dto.AssetClassBreakdown{
			AssetClass: agg.AssetClass.Value(),
			ValueUsd:   agg.Value.Float64(),
			Pct:        pctF,
			Count:      agg.Count,
			Color:      dto.ColorOf(classes.Color(agg.AssetClass)),
			Liquid:     classes.Liquid(agg.AssetClass),
		}
	}

	// ByPlatform, each as the user set it up
	looks := make(map[string]model.PlatformSettings, len(platformSettings))
	for _, ps := range platformSettings {
		looks[ps.Key] = ps
	}
	byPlatformDTOs := make([]dto.PlatformBreakdown, len(byPlatform))
	for i, agg := range byPlatform {
		look := looks[agg.Key]
		if look.Type != nil {
			agg.Type = *look.Type
		}
		pct := agg.Value.PercentOf(assets)
		var pctF float64
		if pct != nil {
			pctF, _ = pct.Round(1).Float64()
		}
		byPlatformDTOs[i] = dto.PlatformBreakdown{
			Name:       agg.Name.Value(),
			Type:       agg.Type.Value(),
			ValueUsd:   agg.Value.Float64(),
			Pct:        pctF,
			Count:      agg.Count,
			AvatarText: look.AvatarText,
			Color:      dto.ColorOf(look.Color),
			TextColor:  dto.ColorOf(look.TextColor),
		}
	}

	expected := domainService.CalculateExpectedReturn(classes.WithClassReturns(breakdown.Returns))
	expectedDTO := dto.ExpectedReturnDTO{
		CoveragePct: expected.CoveragePct.InexactFloat64(),
		AnnualUsd:   expected.Annual.Float64(),
	}
	if expected.WeightedPct != nil {
		pct := expected.WeightedPct.InexactFloat64()
		expectedDTO.WeightedPct = &pct
	}

	return dto.WealthSummaryResponse{
		NetWorth: dto.NetWorthDTO{
			Usd: netWorth.Float64(),
		},
		Assets: dto.AssetsDTO{Usd: assets.Float64()},
		Debts: dto.DebtsDTO{
			Usd:               debts.Balance.Float64(),
			Count:             debts.Count,
			MonthlyPaymentUsd: debts.MonthlyPayment.Float64(),
		},
		HoldingsCount:  totalCount,
		ExpectedReturn: expectedDTO,
		Ytd:            ytdDTO,
		Liquidity:      liquidityDTO,
		ByAssetClass:   byAssetClassDTOs,
		ByPlatform:     byPlatformDTOs,
	}, nil
}

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// How the user set up their classes and platforms shows in the summary, the projection and the holdings.

func TestWealthQueryService_SummaryUsesTheUsersClassesAndPlatforms(t *testing.T) {
	user := model.NewUserId(uuid.New())
	classes, looks := newMockClassSettingsRepo(), newMockPlatformSettingsRepo()
	art, red := model.MustAssetClass("Art"), model.MustColor("#ff0000")
	five := decimal.NewFromInt(5)
	classes.settings[classSettingsKey(user, art)] = model.AssetClassSettings{UserId: user, Name: art, Liquid: ptr(true), Color: &red, ExpectedReturnPct: &five}
	equity := model.MustAssetClass("Equity")
	classes.settings[classSettingsKey(user, equity)] = model.AssetClassSettings{UserId: user, Name: equity, Liquid: ptr(false)}
	exchange := model.MustPlatformType("Exchange")
	looks.settings[user.String()+"/balanz"] = model.PlatformSettings{UserId: user, Key: "balanz", Type: &exchange, AvatarText: ptr("BZ"), Color: &red}

	agg := &mockWealthAggregationPort{
		assets: model.MustMoneyFromFloat(100),
		byAssetClass: []outbound.AssetClassAggregate{
			{AssetClass: equity, Value: model.MustMoneyFromFloat(50), Count: 1},
			{AssetClass: art, Value: model.MustMoneyFromFloat(30), Count: 1},
			{AssetClass: model.MustAssetClass("Cash"), Value: model.MustMoneyFromFloat(20), Count: 1},
		},
		byPlatform: []outbound.PlatformAggregate{
			{Key: "balanz", Name: model.MustPlatformName("Balanz"), Type: model.MustPlatformType("Broker"), Value: model.MustMoneyFromFloat(80), Count: 2},
			{Key: "nexo", Name: model.MustPlatformName("Nexo"), Type: model.PlatformTypeOther, Value: model.MustMoneyFromFloat(20), Count: 1},
		},
		returns: []model.HoldingReturn{
			{Value: model.MustMoneyFromFloat(50), Class: equity},
			{Value: model.MustMoneyFromFloat(30), Class: art},                          // 5%, from Art
			{Value: model.MustMoneyFromFloat(20), Class: model.MustAssetClass("Cash")}, // none
		},
	}
	svc := service.NewWealthQueryService(agg, newMockSnapshotRepo(), classes, looks, fixedClock(time.Now()), service.NewClassDefaults(nil, nil))
	summary, err := svc.GetSummary(context.Background(), user)
	require.NoError(t, err)

	// Art (30) and Cash (20) are liquid; Equity was set not to be.
	assert.Equal(t, 50.0, summary.Liquidity.LiquidPct)
	assert.Equal(t, []string{"Cash", "Index Fund", "Crypto", "Art"}, summary.Liquidity.LiquidAssetClasses)
	assert.False(t, summary.ByAssetClass[0].Liquid)
	assert.Nil(t, summary.ByAssetClass[0].Color)
	assert.True(t, summary.ByAssetClass[1].Liquid)
	assert.Equal(t, "#ff0000", *summary.ByAssetClass[1].Color)

	assert.Equal(t, "Exchange", summary.ByPlatform[0].Type)
	assert.Equal(t, "BZ", *summary.ByPlatform[0].AvatarText)
	assert.Equal(t, "#ff0000", *summary.ByPlatform[0].Color)
	assert.Equal(t, "Other", summary.ByPlatform[1].Type)
	assert.Nil(t, summary.ByPlatform[1].AvatarText)
	assert.Nil(t, summary.ByPlatform[1].Color)

	// 30 × 5% / 100: Art's holding counts with its class's return.
	assert.Equal(t, 1.5, *summary.ExpectedReturn.WeightedPct)
	assert.Equal(t, 30.0, summary.ExpectedReturn.CoveragePct)
}

func TestWealthQueryService_SummaryFailsWithTheSettings(t *testing.T) {
	classes, looks := newMockClassSettingsRepo(), newMockPlatformSettingsRepo()
	looks.findErr = assert.AnError
	svc := service.NewWealthQueryService(&mockWealthAggregationPort{}, newMockSnapshotRepo(), classes, looks, fixedClock(time.Now()), service.NewClassDefaults(nil, nil))
	_, err := svc.GetSummary(context.Background(), model.NewUserId(uuid.New()))
	assert.ErrorIs(t, err, assert.AnError)
	looks.findErr, classes.findErr = nil, assert.AnError
	_, err = svc.GetSummary(context.Background(), model.NewUserId(uuid.New()))
	assert.ErrorIs(t, err, assert.AnError)
}

func TestProjectionService_HoldingsWithoutAReturnCountWithTheirClasses(t *testing.T) {
	user := model.NewUserId(uuid.New())
	classes := newMockClassSettingsRepo()
	stocks, ten := model.MustAssetClass("Stocks"), decimal.NewFromInt(10)
	classes.settings[classSettingsKey(user, stocks)] = model.AssetClassSettings{UserId: user, Name: stocks, ExpectedReturnPct: &ten}
	four := decimal.NewFromInt(4)
	agg := &mockWealthAggregationPort{
		assets: model.MustMoneyFromFloat(100),
		returns: []model.HoldingReturn{
			{Value: model.MustMoneyFromFloat(50), Class: stocks},             // 10%, from Stocks
			{Value: model.MustMoneyFromFloat(50), Class: stocks, Pct: &four}, // its own wins
		},
	}
	svc := service.NewProjectionService(agg, newMockDebtRepo(), classes, fixedClock(time.Now()))
	result, err := svc.Project(context.Background(), inbound.ProjectionRequest{UserId: user, Years: 1})
	require.NoError(t, err)
	assert.Equal(t, "7", result.PortfolioYieldPct.String())

	classes.findErr = assert.AnError
	_, err = svc.Project(context.Background(), inbound.ProjectionRequest{UserId: user, Years: 1})
	assert.ErrorIs(t, err, assert.AnError)
}

func TestHoldingService_EffectiveReturnFallsBackToTheClasses(t *testing.T) {
	f := newLedgerFixture(fixedClock(time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)))
	user := model.NewUserId(uuid.New())
	ctx := context.Background()
	equity, eight := model.MustAssetClass("Equity"), decimal.NewFromInt(8)
	f.classes.settings[classSettingsKey(user, equity)] = model.AssetClassSettings{UserId: user, Name: equity, ExpectedReturnPct: &eight}

	created, err := f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{UserId: user, Name: "SPY", AssetClass: "Equity", Platform: "Balanz", ValueUsd: 100})
	require.NoError(t, err)
	assert.Nil(t, created.ExpectedReturnPct)
	assert.Equal(t, "8", created.EffectiveReturnPct().String())

	all, err := f.holdingSvc.GetAllHoldings(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, "8", all[0].EffectiveReturnPct().String())

	// Its own wins; another class has none.
	updated, err := f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: user, Id: created.Id, ExpectedReturnPct: set(3.0)})
	require.NoError(t, err)
	assert.Equal(t, "3", updated.EffectiveReturnPct().String())
	updated, err = f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: user, Id: created.Id, AssetClass: ptr("Gold"),
		ExpectedReturnPct: inbound.Change[float64]{Set: true}})
	require.NoError(t, err)
	assert.Nil(t, updated.EffectiveReturnPct())

	// Back to Equity, with nothing else changing: its class's again.
	updated, err = f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: user, Id: created.Id, AssetClass: ptr("Equity")})
	require.NoError(t, err)
	assert.Equal(t, "8", updated.EffectiveReturnPct().String())
	unchanged, err := f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: user, Id: created.Id, Name: ptr("SPY")})
	require.NoError(t, err)
	assert.Equal(t, "8", unchanged.EffectiveReturnPct().String())

	set, err := f.holdingSvc.SetExpectedReturns(ctx, user, []inbound.ExpectedReturnItem{{HoldingId: created.Id}})
	require.NoError(t, err)
	assert.Equal(t, "8", set[0].EffectiveReturnPct().String())

	f.classes.findErr = assert.AnError
	_, err = f.holdingSvc.GetAllHoldings(ctx, user)
	assert.ErrorIs(t, err, assert.AnError)
	_, err = f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{UserId: user, Name: "QQQ", AssetClass: "Equity", Platform: "Balanz", ValueUsd: 1})
	assert.ErrorIs(t, err, assert.AnError)
	all, _ = f.holdings.FindAll(ctx, user)
	assert.Len(t, all, 1, "the create was rolled back")
}

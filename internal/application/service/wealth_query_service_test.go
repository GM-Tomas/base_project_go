package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWealthQueryService_GetSummary(t *testing.T) {
	snapshotRepo := newMockSnapshotRepo()
	wealthAgg := &mockWealthAggregationPort{
		netWorth: model.MustMoneyFromFloat(100000.0),
		byAssetClass: []outbound.AssetClassAggregate{
			{AssetClass: model.MustAssetClass("Cash"), Value: model.MustMoneyFromFloat(40000.0), Count: 2},
			{AssetClass: model.MustAssetClass("Equity"), Value: model.MustMoneyFromFloat(60000.0), Count: 3},
		},
		byPlatform: []outbound.PlatformAggregate{
			{Name: model.MustPlatformName("Balanz"), Type: model.MustPlatformType("Broker"), Value: model.MustMoneyFromFloat(60000.0), Count: 3},
			{Name: model.MustPlatformName("Mercado Pago"), Type: model.MustPlatformType("Wallet"), Value: model.MustMoneyFromFloat(40000.0), Count: 2},
			{Name: model.MustPlatformName("Binance"), Type: model.MustPlatformType("Exchange"), Value: model.ZeroMoney, Count: 0},
		},
	}

	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewWealthQueryService(wealthAgg, snapshotRepo, fixedClock(now), nil)

	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	summary, err := svc.GetSummary(ctx, userId)
	require.NoError(t, err)

	assert.Equal(t, 100000.00, summary.NetWorth.Usd)
	assert.Equal(t, 5, summary.HoldingsCount)
	assert.Equal(t, 100.0, summary.Liquidity.LiquidPct)
	assert.Equal(t, 0.0, summary.Liquidity.IlliquidPct)
	assert.Len(t, summary.ByAssetClass, 2)
	assert.Len(t, summary.ByPlatform, 3) // includes 0 value platform
}

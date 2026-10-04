package service_test

import (
	"context"
	"errors"
	"sync"
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

// The summary's reads, each running onRead first.
type slowAggregation struct {
	mockWealthAggregationPort
	onRead func()
}

func (a *slowAggregation) Breakdown(ctx context.Context, userId model.UserId) (outbound.WealthBreakdown, error) {
	a.onRead()
	return a.mockWealthAggregationPort.Breakdown(ctx, userId)
}

type slowSnapshots struct {
	*mockSnapshotRepo
	onRead         func()
	firstOfYearErr error
}

func (s *slowSnapshots) FindFirstOfYear(ctx context.Context, userId model.UserId, year int) (*model.NetWorthSnapshot, error) {
	s.onRead()
	if s.firstOfYearErr != nil {
		return nil, s.firstOfYearErr
	}
	return s.mockSnapshotRepo.FindFirstOfYear(ctx, userId, year)
}

func (s *slowSnapshots) FindEarliest(ctx context.Context, userId model.UserId) (*model.NetWorthSnapshot, error) {
	s.onRead()
	return s.mockSnapshotRepo.FindEarliest(ctx, userId)
}

func TestWealthQueryService_GetSummary_ReadsEverythingAtOnce(t *testing.T) {
	// Each read waits until all three have started: read one after another, the summary would never come.
	var started sync.WaitGroup
	started.Add(3)
	onRead := func() {
		started.Done()
		started.Wait()
	}
	agg := &slowAggregation{mockWealthAggregationPort: mockWealthAggregationPort{netWorth: model.MustMoneyFromFloat(10)}, onRead: onRead}
	snapshots := &slowSnapshots{mockSnapshotRepo: newMockSnapshotRepo(), onRead: onRead}
	svc := service.NewWealthQueryService(agg, snapshots, fixedClock(time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)), nil)

	done := make(chan error, 1)
	go func() {
		summary, err := svc.GetSummary(context.Background(), model.NewUserId(uuid.New()))
		if err == nil && summary.NetWorth.Usd != 10 {
			err = errors.New("wrong net worth")
		}
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the summary's reads didn't run at the same time")
	}
}

func TestWealthQueryService_GetSummary_AFailingReadFailsItWithItsError(t *testing.T) {
	broken := errors.New("snapshots unreachable")
	snapshots := &slowSnapshots{mockSnapshotRepo: newMockSnapshotRepo(), onRead: func() {}, firstOfYearErr: broken}
	svc := service.NewWealthQueryService(&mockWealthAggregationPort{}, snapshots, fixedClock(time.Now()), nil)

	_, err := svc.GetSummary(context.Background(), model.NewUserId(uuid.New()))

	assert.ErrorIs(t, err, broken)
}

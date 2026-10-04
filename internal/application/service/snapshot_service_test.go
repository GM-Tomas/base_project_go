package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSnapshotRepo struct {
	snapshots   []model.NetWorthSnapshot
	firstOfYear *model.NetWorthSnapshot
	earliest    *model.NetWorthSnapshot
	countErr    error
}

func newMockSnapshotRepo() *mockSnapshotRepo {
	return &mockSnapshotRepo{}
}

func (m *mockSnapshotRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.NetWorthSnapshot, error) {
	return m.snapshots, nil
}

func (m *mockSnapshotRepo) Count(ctx context.Context, userId model.UserId) (int64, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	var n int64
	for _, s := range m.snapshots {
		if s.UserId == userId {
			n++
		}
	}
	return n, nil
}

func (m *mockSnapshotRepo) Save(ctx context.Context, snapshot model.NetWorthSnapshot) (model.NetWorthSnapshot, error) {
	m.snapshots = append(m.snapshots, snapshot)
	return snapshot, nil
}

func (m *mockSnapshotRepo) ExistsAt(ctx context.Context, userId model.UserId, capturedAt time.Time) (bool, error) {
	for _, s := range m.snapshots {
		if s.UserId.UUID() == userId.UUID() && s.CapturedAt.Equal(capturedAt) {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockSnapshotRepo) FindFirstOfYear(ctx context.Context, userId model.UserId, year int) (*model.NetWorthSnapshot, error) {
	return m.firstOfYear, nil
}

func (m *mockSnapshotRepo) FindEarliest(ctx context.Context, userId model.UserId) (*model.NetWorthSnapshot, error) {
	return m.earliest, nil
}

type mockWealthAggregationPort struct {
	netWorth     model.Money
	byAssetClass []outbound.AssetClassAggregate
	byPlatform   []outbound.PlatformAggregate
}

func (m *mockWealthAggregationPort) NetWorth(ctx context.Context, userId model.UserId) (model.Money, error) {
	return m.netWorth, nil
}

func (m *mockWealthAggregationPort) ByAssetClass(ctx context.Context, userId model.UserId) ([]outbound.AssetClassAggregate, error) {
	return m.byAssetClass, nil
}

func (m *mockWealthAggregationPort) ByPlatform(ctx context.Context, userId model.UserId) ([]outbound.PlatformAggregate, error) {
	return m.byPlatform, nil
}

func TestSnapshotService_CreateAndGet(t *testing.T) {
	snapshotRepo := newMockSnapshotRepo()
	wealthAgg := &mockWealthAggregationPort{
		netWorth: model.MustMoneyFromFloat(100000.0),
	}
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewSnapshotService(snapshotRepo, wealthAgg, fixedClock(now))

	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	// 1. Create snapshot
	snap, err := svc.CreateSnapshot(ctx, userId)
	require.NoError(t, err)
	assert.Equal(t, "100000.00", snap.TotalValue.String())
	assert.Equal(t, now, snap.CapturedAt)

	// 2. Duplicate snapshot in same second -> 409
	_, err = svc.CreateSnapshot(ctx, userId)
	assert.Error(t, err)

	// 3. GetSnapshots
	snaps, err := svc.GetSnapshots(ctx, userId)
	require.NoError(t, err)
	assert.Len(t, snaps, 1)
	assert.Nil(t, snaps[0].ChangePctFromPrevious)
}

func TestSnapshotService_CapsSnapshotsPerUser(t *testing.T) {
	snapshotRepo := newMockSnapshotRepo()
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewSnapshotService(snapshotRepo, &mockWealthAggregationPort{netWorth: model.ZeroMoney}, fixedClock(now))
	full, other := model.NewUserId(uuid.New()), model.NewUserId(uuid.New())

	for i := 0; i < model.MaxSnapshotsPerUser; i++ {
		snapshotRepo.snapshots = append(snapshotRepo.snapshots,
			model.NewNetWorthSnapshot(model.NewSnapshotId(), full, now.Add(-time.Duration(i+1)*time.Hour), model.ZeroMoney))
	}

	_, err := svc.CreateSnapshot(context.Background(), full)
	assert.ErrorAs(t, err, &appErrors.LimitExceededError{})

	_, err = svc.CreateSnapshot(context.Background(), other)
	assert.NoError(t, err, "the cap is per account")

	snapshotRepo.countErr = errors.New("db down")
	_, err = svc.CreateSnapshot(context.Background(), model.NewUserId(uuid.New()))
	assert.EqualError(t, err, "db down")
}

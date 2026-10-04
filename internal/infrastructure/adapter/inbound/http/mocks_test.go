package http_test

import (
	"context"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
)

type mockHoldingRepo struct {
	holdings     map[string]model.Holding
	assetClasses []model.AssetClass
}

func newMockHoldingRepo() *mockHoldingRepo {
	return &mockHoldingRepo{
		holdings: make(map[string]model.Holding),
	}
}

func (m *mockHoldingRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.Holding, error) {
	var list []model.Holding
	for _, h := range m.holdings {
		if h.UserId.UUID() == userId.UUID() {
			list = append(list, h)
		}
	}
	return list, nil
}

func (m *mockHoldingRepo) Count(ctx context.Context, userId model.UserId) (int64, error) {
	all, _ := m.FindAll(ctx, userId)
	return int64(len(all)), nil
}

func (m *mockHoldingRepo) Save(ctx context.Context, holding model.Holding) (model.Holding, error) {
	m.holdings[holding.Id.String()] = holding
	return holding, nil
}

func (m *mockHoldingRepo) DeleteById(ctx context.Context, userId model.UserId, id model.HoldingId) (bool, error) {
	h, ok := m.holdings[id.String()]
	if !ok || h.UserId.UUID() != userId.UUID() {
		return false, nil
	}
	delete(m.holdings, id.String())
	return true, nil
}

func (m *mockHoldingRepo) AssetClassesInUse(ctx context.Context, userId model.UserId) ([]model.AssetClass, error) {
	return m.assetClasses, nil
}

type mockPlatformRepo struct {
	platforms map[string]model.Platform
	holdings  *mockHoldingRepo
}

func newMockPlatformRepo(holdings *mockHoldingRepo) *mockPlatformRepo {
	return &mockPlatformRepo{
		platforms: make(map[string]model.Platform),
		holdings:  holdings,
	}
}

func (m *mockPlatformRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.Platform, error) {
	var list []model.Platform
	for _, p := range m.platforms {
		if p.UserId.UUID() == userId.UUID() {
			list = append(list, p)
		}
	}
	return list, nil
}

func (m *mockPlatformRepo) EnsureExists(ctx context.Context, userId model.UserId, name model.PlatformName, now time.Time) (model.PlatformName, error) {
	for _, p := range m.platforms {
		if p.UserId.UUID() == userId.UUID() && strings.EqualFold(p.Name.Value(), name.Value()) {
			return p.Name, nil
		}
	}
	m.platforms[name.Value()] = model.NewPlatform(userId, name, model.PlatformTypeOther, now)
	return name, nil
}

func (m *mockPlatformRepo) DeleteUnused(ctx context.Context, userId model.UserId, now time.Time) error {
	used := map[string]bool{}
	for _, h := range m.holdings.holdings {
		used[h.Platform.Value()] = true
	}
	for name, p := range m.platforms {
		if p.UserId.UUID() == userId.UUID() && !used[name] {
			delete(m.platforms, name)
		}
	}
	return nil
}

type mockSnapshotRepo struct {
	snapshots   []model.NetWorthSnapshot
	firstOfYear *model.NetWorthSnapshot
	earliest    *model.NetWorthSnapshot
}

func newMockSnapshotRepo() *mockSnapshotRepo {
	return &mockSnapshotRepo{}
}

func (m *mockSnapshotRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.NetWorthSnapshot, error) {
	return m.snapshots, nil
}

func (m *mockSnapshotRepo) Count(ctx context.Context, userId model.UserId) (int64, error) {
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

func (m *mockSnapshotRepo) DeleteById(ctx context.Context, userId model.UserId, id model.SnapshotId) (bool, error) {
	for i, s := range m.snapshots {
		if s.Id == id && s.UserId == userId {
			m.snapshots = append(m.snapshots[:i], m.snapshots[i+1:]...)
			return true, nil
		}
	}
	return false, nil
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

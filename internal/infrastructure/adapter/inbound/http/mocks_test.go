package http_test

import (
	"context"
	"sort"
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

// mockPlatformRepo derives platforms from the holdings, like the real one.
type mockPlatformRepo struct {
	holdings *mockHoldingRepo
}

func newMockPlatformRepo(holdings *mockHoldingRepo) *mockPlatformRepo {
	return &mockPlatformRepo{holdings: holdings}
}

func (m *mockPlatformRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.Platform, error) {
	seen := map[string]bool{}
	var list []model.Platform
	for _, h := range m.holdings.holdings {
		if h.UserId != userId || seen[strings.ToLower(h.Platform.Value())] {
			continue
		}
		seen[strings.ToLower(h.Platform.Value())] = true
		list = append(list, model.NewPlatform(userId, h.Platform, model.PlatformTypeOther, h.CreatedAt))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name.Value() < list[j].Name.Value() })
	return list, nil
}

func (m *mockPlatformRepo) Canonical(ctx context.Context, userId model.UserId, name model.PlatformName) (model.PlatformName, error) {
	for _, h := range m.holdings.holdings {
		if h.UserId == userId && strings.EqualFold(h.Platform.Value(), name.Value()) {
			return h.Platform, nil
		}
	}
	return name, nil
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

func (m *mockWealthAggregationPort) Breakdown(ctx context.Context, userId model.UserId) (outbound.WealthBreakdown, error) {
	return outbound.WealthBreakdown{NetWorth: m.netWorth, ByAssetClass: m.byAssetClass, ByPlatform: m.byPlatform}, nil
}

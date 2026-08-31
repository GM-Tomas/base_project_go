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

func (m *mockHoldingRepo) FindAll(ctx context.Context, userId model.UserId, assetClass *model.AssetClass, platform *model.PlatformName) ([]model.Holding, error) {
	var list []model.Holding
	for _, h := range m.holdings {
		if h.UserId.UUID() != userId.UUID() {
			continue
		}
		if assetClass != nil && h.AssetClass.Value() != assetClass.Value() {
			continue
		}
		if platform != nil && h.Platform.Value() != platform.Value() {
			continue
		}
		list = append(list, h)
	}
	return list, nil
}

func (m *mockHoldingRepo) FindById(ctx context.Context, userId model.UserId, id model.HoldingId) (*model.Holding, error) {
	h, ok := m.holdings[id.String()]
	if !ok || h.UserId.UUID() != userId.UUID() {
		return nil, nil
	}
	return &h, nil
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
	holdings  map[string]int
}

func newMockPlatformRepo() *mockPlatformRepo {
	return &mockPlatformRepo{
		platforms: make(map[string]model.Platform),
		holdings:  make(map[string]int),
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

func (m *mockPlatformRepo) FindByName(ctx context.Context, userId model.UserId, name model.PlatformName) (*model.Platform, error) {
	for _, p := range m.platforms {
		if p.UserId.UUID() == userId.UUID() && strings.EqualFold(p.Name.Value(), name.Value()) {
			return &p, nil
		}
	}
	return nil, nil
}

func (m *mockPlatformRepo) EnsureExists(ctx context.Context, userId model.UserId, name model.PlatformName, now time.Time) (model.PlatformName, error) {
	existing, _ := m.FindByName(ctx, userId, name)
	if existing != nil {
		return existing.Name, nil
	}
	p := model.NewPlatform(userId, name, model.PlatformTypeOther, now)
	m.platforms[name.Value()] = p
	return name, nil
}

func (m *mockPlatformRepo) Save(ctx context.Context, platform model.Platform) (model.Platform, error) {
	m.platforms[platform.Name.Value()] = platform
	return platform, nil
}

func (m *mockPlatformRepo) Update(ctx context.Context, userId model.UserId, currentName model.PlatformName, newName *model.PlatformName, newType *model.PlatformType) (*model.Platform, error) {
	p, _ := m.FindByName(ctx, userId, currentName)
	if p == nil {
		return nil, nil
	}
	delete(m.platforms, p.Name.Value())
	if newName != nil {
		p.Name = *newName
	}
	if newType != nil {
		p.Type = *newType
	}
	m.platforms[p.Name.Value()] = *p
	return p, nil
}

func (m *mockPlatformRepo) DeleteByName(ctx context.Context, userId model.UserId, name model.PlatformName) (bool, error) {
	p, _ := m.FindByName(ctx, userId, name)
	if p == nil {
		return false, nil
	}
	delete(m.platforms, p.Name.Value())
	return true, nil
}

func (m *mockPlatformRepo) CountHoldings(ctx context.Context, userId model.UserId, name model.PlatformName) (int, error) {
	return m.holdings[name.Value()], nil
}

type mockSnapshotRepo struct {
	snapshots   []model.NetWorthSnapshot
	firstOfYear *model.NetWorthSnapshot
	earliest    *model.NetWorthSnapshot
}

func newMockSnapshotRepo() *mockSnapshotRepo {
	return &mockSnapshotRepo{}
}

func (m *mockSnapshotRepo) FindAll(ctx context.Context, userId model.UserId, from *time.Time, to *time.Time) ([]model.NetWorthSnapshot, error) {
	return m.snapshots, nil
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

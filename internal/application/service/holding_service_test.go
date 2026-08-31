package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func fixedClock(t time.Time) service.Clock {
	return func() time.Time { return t }
}

func TestHoldingService_CRUD(t *testing.T) {
	holdingRepo := newMockHoldingRepo()
	platformRepo := newMockPlatformRepo()
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewHoldingService(holdingRepo, platformRepo, fixedClock(now))

	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	// 1. Create holding (auto-creates platform)
	created, err := svc.CreateHolding(ctx, inbound.CreateHoldingCommand{
		UserId:     userId,
		Name:       "NVDA",
		AssetClass: "Equity",
		Platform:   "Balanz",
		ValueUsd:   10557.00,
	})
	require.NoError(t, err)
	assert.Equal(t, "NVDA", created.Name)
	assert.Equal(t, "Equity", created.AssetClass.Value())
	assert.Equal(t, "Balanz", created.Platform.Value())
	assert.Equal(t, "10557.00", created.Value.String())

	// Platform should exist
	p, err := platformRepo.FindByName(ctx, userId, model.MustPlatformName("Balanz"))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "Other", p.Type.Value())

	// 2. Get holding by ID
	fetched, err := svc.GetHoldingById(ctx, userId, created.Id)
	require.NoError(t, err)
	assert.Equal(t, created.Id, fetched.Id)

	// 3. GetAllHoldings
	all, err := svc.GetAllHoldings(ctx, userId, nil, nil)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	// 4. Update holding
	newVal := 12000.00
	updated, err := svc.UpdateHolding(ctx, inbound.PatchHoldingCommand{
		UserId:   userId,
		Id:       created.Id,
		ValueUsd: &newVal,
	})
	require.NoError(t, err)
	assert.Equal(t, "12000.00", updated.Value.String())

	// 5. Delete holding
	err = svc.DeleteHolding(ctx, userId, created.Id)
	require.NoError(t, err)

	// 6. Delete again -> 404
	err = svc.DeleteHolding(ctx, userId, created.Id)
	assert.Error(t, err)
}

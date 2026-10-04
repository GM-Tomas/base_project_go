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

func (m *mockHoldingRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.Holding, error) {
	var list []model.Holding
	for _, h := range m.holdings {
		if h.UserId.UUID() == userId.UUID() {
			list = append(list, h)
		}
	}
	return list, nil
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
	holdings  *mockHoldingRepo // to know which platforms are still referenced
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

func (m *mockPlatformRepo) DeleteUnused(ctx context.Context, userId model.UserId) error {
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

func fixedClock(t time.Time) service.Clock {
	return func() time.Time { return t }
}

func TestHoldingService_Lifecycle(t *testing.T) {
	holdingRepo := newMockHoldingRepo()
	platformRepo := newMockPlatformRepo(holdingRepo)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewHoldingService(holdingRepo, platformRepo, fixedClock(now))

	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	create := func(name, platform string) model.Holding {
		h, err := svc.CreateHolding(ctx, inbound.CreateHoldingCommand{
			UserId: userId, Name: name, AssetClass: "Equity", Platform: platform, ValueUsd: 10557.00,
		})
		require.NoError(t, err)
		return h
	}

	// 1. Create reuses an existing platform case-insensitively, else auto-creates it.
	nvda := create("NVDA", "Balanz")
	assert.Equal(t, "NVDA", nvda.Name)
	assert.Equal(t, "10557.00", nvda.Value.String())
	aapl := create("AAPL", "balanz")
	assert.Equal(t, "Balanz", aapl.Platform.Value())
	assert.Len(t, platformRepo.platforms, 1)
	assert.Equal(t, "Other", platformRepo.platforms["Balanz"].Type.Value())

	all, err := svc.GetAllHoldings(ctx, userId)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// 2. Deleting one holding keeps the platform while another holding still uses it.
	require.NoError(t, svc.DeleteHolding(ctx, userId, nvda.Id))
	assert.Len(t, platformRepo.platforms, 1)

	// 3. Deleting the last one prunes the platform (the UI has no way to delete it).
	require.NoError(t, svc.DeleteHolding(ctx, userId, aapl.Id))
	assert.Empty(t, platformRepo.platforms)

	// 4. Delete again -> not found
	assert.Error(t, svc.DeleteHolding(ctx, userId, aapl.Id))
}

package service_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockHoldingRepo struct {
	holdings      map[string]model.Holding
	assetClasses  []model.AssetClass
	countErr      error
	countErrAfter int // Count calls that still succeed before countErr kicks in
	countCalls    int
	deleteErr     error
	beforeSave    func() // lands a "concurrent" request between the service's checks and its insert
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
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.countCalls++
	if m.countErr != nil && m.countCalls > m.countErrAfter {
		return 0, m.countErr
	}
	all, _ := m.FindAll(ctx, userId)
	return int64(len(all)), nil
}

func (m *mockHoldingRepo) Save(ctx context.Context, holding model.Holding) (model.Holding, error) {
	if m.beforeSave != nil {
		m.beforeSave()
	}
	m.holdings[holding.Id.String()] = holding
	return holding, nil
}

func (m *mockHoldingRepo) DeleteById(ctx context.Context, userId model.UserId, id model.HoldingId) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if m.deleteErr != nil {
		return false, m.deleteErr
	}
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
	platforms := func() []model.Platform {
		list, err := platformRepo.FindAll(ctx, userId)
		require.NoError(t, err)
		return list
	}
	require.Len(t, platforms(), 1)
	assert.Equal(t, "Other", platforms()[0].Type.Value())

	all, err := svc.GetAllHoldings(ctx, userId)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// 2. Deleting one holding keeps the platform while another holding still uses it.
	require.NoError(t, svc.DeleteHolding(ctx, userId, nvda.Id))
	assert.Len(t, platforms(), 1)

	// 3. Deleting the last one prunes the platform (the UI has no way to delete it).
	require.NoError(t, svc.DeleteHolding(ctx, userId, aapl.Id))
	assert.Empty(t, platforms())

	// 4. Delete again -> not found
	assert.Error(t, svc.DeleteHolding(ctx, userId, aapl.Id))
}

func TestHoldingService_RejectedCreateLeavesNoPlatformBehind(t *testing.T) {
	holdingRepo := newMockHoldingRepo()
	platformRepo := newMockPlatformRepo(holdingRepo)
	svc := service.NewHoldingService(holdingRepo, platformRepo, fixedClock(time.Now()))
	userId := model.NewUserId(uuid.New())

	for name, cmd := range map[string]inbound.CreateHoldingCommand{
		"name too long":        {Name: strings.Repeat("x", model.MaxHoldingNameLength+1), AssetClass: "Cash", Platform: "Brand New", ValueUsd: 1},
		"asset class too long": {Name: "x", AssetClass: strings.Repeat("x", model.MaxAssetClassLength+1), Platform: "Brand New", ValueUsd: 1},
		"negative value":       {Name: "x", AssetClass: "Cash", Platform: "Brand New", ValueUsd: -1},
		"blank platform":       {Name: "x", AssetClass: "Cash", Platform: "  ", ValueUsd: 1},
	} {
		t.Run(name, func(t *testing.T) {
			cmd.UserId = userId
			_, err := svc.CreateHolding(context.Background(), cmd)
			assert.Error(t, err)
			assert.Empty(t, holdingRepo.holdings)
		})
	}
}

func TestHoldingService_CapsHoldingsPerUser(t *testing.T) {
	holdingRepo := newMockHoldingRepo()
	platformRepo := newMockPlatformRepo(holdingRepo)
	svc := service.NewHoldingService(holdingRepo, platformRepo, fixedClock(time.Now()))
	full, other := model.NewUserId(uuid.New()), model.NewUserId(uuid.New())

	for i := 0; i < model.MaxHoldingsPerUser; i++ {
		h := model.Holding{Id: model.NewHoldingId(), UserId: full, Platform: model.MustPlatformName("Bank")}
		holdingRepo.holdings[h.Id.String()] = h
	}
	cmd := func(u model.UserId) inbound.CreateHoldingCommand {
		return inbound.CreateHoldingCommand{UserId: u, Name: "One more", AssetClass: "Cash", Platform: "New Bank", ValueUsd: 1}
	}

	_, err := svc.CreateHolding(context.Background(), cmd(full))
	assert.ErrorAs(t, err, &appErrors.LimitExceededError{})
	platforms, _ := platformRepo.FindAll(context.Background(), full)
	require.Len(t, platforms, 1)
	assert.Equal(t, "Bank", platforms[0].Name.Value(), "no platform for a refused holding")

	// The cap is per account: everyone else is unaffected.
	_, err = svc.CreateHolding(context.Background(), cmd(other))
	assert.NoError(t, err)

	holdingRepo.countErr = errors.New("db down")
	_, err = svc.CreateHolding(context.Background(), cmd(other))
	assert.EqualError(t, err, "db down")
}

func TestHoldingService_CapHoldsWhenCreatesRace(t *testing.T) {
	for name, deleteErr := range map[string]error{"withdrawn": nil, "withdrawal fails": errors.New("db down")} {
		t.Run(name, func(t *testing.T) {
			holdingRepo := newMockHoldingRepo()
			platformRepo := newMockPlatformRepo(holdingRepo)
			svc := service.NewHoldingService(holdingRepo, platformRepo, fixedClock(time.Now()))
			ctx := context.Background()
			user := model.NewUserId(uuid.New())
			add := func() {
				h := model.Holding{Id: model.NewHoldingId(), UserId: user, Name: "other", Platform: model.MustPlatformName("Bank")}
				holdingRepo.holdings[h.Id.String()] = h
			}
			for i := 0; i < model.MaxHoldingsPerUser-1; i++ {
				add()
			}
			// Both requests passed the check at 999; the other one's insert lands first.
			holdingRepo.beforeSave = func() { holdingRepo.beforeSave = nil; add() }
			holdingRepo.deleteErr = deleteErr

			_, err := svc.CreateHolding(ctx, inbound.CreateHoldingCommand{
				UserId: user, Name: "Mine", AssetClass: "Cash", Platform: "Fresh", ValueUsd: 1,
			})

			mine := 0
			for _, h := range holdingRepo.holdings {
				if h.Name == "Mine" {
					mine++
				}
			}
			if deleteErr != nil {
				// It couldn't be taken back, so it stands and is reported as created: a 500 would only
				// invite a retry that stores it twice.
				assert.NoError(t, err)
				assert.Equal(t, 1, mine)
				return
			}
			assert.ErrorAs(t, err, &appErrors.LimitExceededError{})
			n, _ := holdingRepo.Count(ctx, user)
			assert.Equal(t, int64(model.MaxHoldingsPerUser), n, "ours was withdrawn, so the user stays at the cap")
			assert.Zero(t, mine)
		})
	}
}

func TestHoldingService_WithdrawsTheHoldingWhenTheRecountFails(t *testing.T) {
	holdingRepo := newMockHoldingRepo()
	svc := service.NewHoldingService(holdingRepo, newMockPlatformRepo(holdingRepo), fixedClock(time.Now()))
	holdingRepo.countErr, holdingRepo.countErrAfter = errors.New("db blip"), 1

	_, err := svc.CreateHolding(context.Background(), inbound.CreateHoldingCommand{
		UserId: model.NewUserId(uuid.New()), Name: "x", AssetClass: "Cash", Platform: "Bank", ValueUsd: 1,
	})

	assert.EqualError(t, err, "db blip", "fail closed: the cap couldn't be confirmed")
	assert.Empty(t, holdingRepo.holdings, "and the client's error matches what's stored")
}

func TestHoldingService_ClientHangingUpAfterTheInsertDoesNotSkipTheCap(t *testing.T) {
	holdingRepo := newMockHoldingRepo()
	svc := service.NewHoldingService(holdingRepo, newMockPlatformRepo(holdingRepo), fixedClock(time.Now()))
	user := model.NewUserId(uuid.New())
	add := func() {
		h := model.Holding{Id: model.NewHoldingId(), UserId: user, Name: "other", Platform: model.MustPlatformName("Bank")}
		holdingRepo.holdings[h.Id.String()] = h
	}
	for i := 0; i < model.MaxHoldingsPerUser-1; i++ {
		add()
	}
	ctx, hangUp := context.WithCancel(context.Background())
	// A racing request lands, and this client drops its connection right as its own insert goes in.
	holdingRepo.beforeSave = func() { holdingRepo.beforeSave = nil; add(); hangUp() }

	_, err := svc.CreateHolding(ctx, inbound.CreateHoldingCommand{
		UserId: user, Name: "Mine", AssetClass: "Cash", Platform: "Bank", ValueUsd: 1,
	})

	assert.ErrorAs(t, err, &appErrors.LimitExceededError{})
	n, _ := holdingRepo.Count(context.Background(), user)
	assert.Equal(t, int64(model.MaxHoldingsPerUser), n)
}

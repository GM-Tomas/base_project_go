package service_test

import (
	"context"
	"maps"
	"slices"
	"sort"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
)

type mockMovementRepo struct {
	movements    []model.Movement
	findOverride *model.Movement // what FindById answers, whatever is stored
	saveErr      error
	findErr      error
	listErr      error
	deleteErr    error
}

func (m *mockMovementRepo) Save(ctx context.Context, mv model.Movement) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.movements = append(m.movements, mv)
	return nil
}

func (m *mockMovementRepo) FindById(ctx context.Context, userId model.UserId, id model.MovementId) (*model.Movement, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if m.findOverride != nil {
		return m.findOverride, nil
	}
	for _, mv := range m.movements {
		if mv.Id == id && mv.UserId == userId {
			return &mv, nil
		}
	}
	return nil, nil
}

// List keeps the real one's order (newest first by occurredAt, createdAt, id) and paging, and filters by
// holding and kind (the date range is the Mongo tests' to check).
func (m *mockMovementRepo) List(ctx context.Context, userId model.UserId, filter model.MovementFilter, after *model.MovementCursor, limit int) (outbound.MovementPage, error) {
	if m.listErr != nil {
		return outbound.MovementPage{}, m.listErr
	}
	var list []model.Movement
	for _, mv := range m.movements {
		if mv.UserId != userId {
			continue
		}
		if id := filter.HoldingId; id != nil && !(mv.Holding != nil && mv.Holding.Id == *id) && !(mv.ToHolding != nil && mv.ToHolding.Id == *id) {
			continue
		}
		if len(filter.Kinds) > 0 && !slices.Contains(filter.Kinds, mv.Kind) {
			continue
		}
		list = append(list, mv)
	}
	newer := func(a, b model.Movement) bool {
		if !a.OccurredAt.Equal(b.OccurredAt) {
			return a.OccurredAt.After(b.OccurredAt)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.Id.String() > b.Id.String()
	}
	sort.Slice(list, func(i, j int) bool { return newer(list[i], list[j]) })
	if after != nil {
		cut := model.Movement{OccurredAt: after.OccurredAt, CreatedAt: after.CreatedAt, Id: after.Id}
		for len(list) > 0 && !newer(cut, list[0]) {
			list = list[1:]
		}
	}
	page := outbound.MovementPage{Items: list}
	if len(list) > limit {
		page.Items = list[:limit]
		last := list[limit-1]
		page.Next = &model.MovementCursor{OccurredAt: last.OccurredAt, CreatedAt: last.CreatedAt, Id: last.Id}
	}
	return page, nil
}

func (m *mockMovementRepo) DeleteById(ctx context.Context, userId model.UserId, id model.MovementId) (bool, error) {
	if m.deleteErr != nil {
		return false, m.deleteErr
	}
	for i, mv := range m.movements {
		if mv.Id == id && mv.UserId == userId {
			m.movements = slices.Delete(m.movements, i, i+1)
			return true, nil
		}
	}
	return false, nil
}

type mockQuotaRepo struct {
	counts     map[string]int
	reserveErr error
	releaseErr error
}

func newMockQuotaRepo() *mockQuotaRepo { return &mockQuotaRepo{counts: map[string]int{}} }

func (m *mockQuotaRepo) Reserve(ctx context.Context, userId model.UserId, key string, n, limit int) (bool, error) {
	if m.reserveErr != nil {
		return false, m.reserveErr
	}
	k := userId.String() + "/" + key
	if m.counts[k]+n > limit {
		return false, nil
	}
	m.counts[k] += n
	return true, nil
}

func (m *mockQuotaRepo) Release(ctx context.Context, userId model.UserId, key string, n int) error {
	if m.releaseErr != nil {
		return m.releaseErr
	}
	m.counts[userId.String()+"/"+key] -= n
	return nil
}

func (m *mockQuotaRepo) movementsOf(userId model.UserId) int {
	return m.counts[userId.String()+"/movements"]
}

// fakeTx ends fn like a transaction: when it fails, what it changed in the mocks is rolled back.
type fakeTx struct {
	holdings  *mockHoldingRepo
	movements *mockMovementRepo
	quotas    *mockQuotaRepo
	calls     int
}

func (f *fakeTx) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	f.calls++
	holdings, movements, counts := maps.Clone(f.holdings.holdings), slices.Clone(f.movements.movements), maps.Clone(f.quotas.counts)
	if err := fn(ctx); err != nil {
		f.holdings.holdings, f.movements.movements, f.quotas.counts = holdings, movements, counts
		return err
	}
	return nil
}

// ledgerFixture is the services that write the activity log, over mocks that share one store.
type ledgerFixture struct {
	holdings    *mockHoldingRepo
	platforms   *mockPlatformRepo
	movements   *mockMovementRepo
	quotas      *mockQuotaRepo
	tx          *fakeTx
	holdingSvc  *service.HoldingService
	movementSvc *service.MovementService
}

func newLedgerFixture(clock service.Clock) *ledgerFixture {
	f := &ledgerFixture{holdings: newMockHoldingRepo(), movements: &mockMovementRepo{}, quotas: newMockQuotaRepo()}
	f.platforms = newMockPlatformRepo(f.holdings)
	f.tx = &fakeTx{holdings: f.holdings, movements: f.movements, quotas: f.quotas}
	f.holdingSvc = service.NewHoldingService(f.tx, f.holdings, f.platforms, f.movements, f.quotas, clock)
	f.movementSvc = service.NewMovementService(f.tx, f.holdings, f.platforms, f.movements, f.quotas, clock)
	return f
}

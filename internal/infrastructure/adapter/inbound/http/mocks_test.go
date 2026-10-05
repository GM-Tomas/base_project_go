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

func (m *mockHoldingRepo) FindById(ctx context.Context, userId model.UserId, id model.HoldingId) (*model.Holding, error) {
	h, ok := m.holdings[id.String()]
	if !ok || h.UserId != userId {
		return nil, nil
	}
	return &h, nil
}

func (m *mockHoldingRepo) Update(ctx context.Context, holding model.Holding) (bool, error) {
	h, ok := m.holdings[holding.Id.String()]
	if !ok || h.UserId != holding.UserId {
		return false, nil
	}
	m.holdings[holding.Id.String()] = holding
	return true, nil
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

func (m *mockHoldingRepo) ExistingIds(ctx context.Context, userId model.UserId, ids []model.HoldingId) (map[model.HoldingId]bool, error) {
	existing := map[model.HoldingId]bool{}
	for _, id := range ids {
		if h, ok := m.holdings[id.String()]; ok && h.UserId == userId {
			existing[id] = true
		}
	}
	return existing, nil
}

// mockMovementRepo keeps movements in memory, newest first as the real one lists them, and the last query.
type mockMovementRepo struct {
	movements  []model.Movement
	lastFilter model.MovementFilter
	lastAfter  *model.MovementCursor
	lastLimit  int
}

func (m *mockMovementRepo) Save(ctx context.Context, mv model.Movement) error {
	m.movements = append(m.movements, mv)
	return nil
}

func (m *mockMovementRepo) FindById(ctx context.Context, userId model.UserId, id model.MovementId) (*model.Movement, error) {
	for _, mv := range m.movements {
		if mv.Id == id && mv.UserId == userId {
			return &mv, nil
		}
	}
	return nil, nil
}

// List pages by position: the cursor's id is where the previous page ended (the HTTP tests check the
// cursor round trip and the query parsing, the Mongo tests the real order).
func (m *mockMovementRepo) List(ctx context.Context, userId model.UserId, filter model.MovementFilter, after *model.MovementCursor, limit int) (outbound.MovementPage, error) {
	m.lastFilter, m.lastAfter, m.lastLimit = filter, after, limit
	var list []model.Movement
	for i := len(m.movements) - 1; i >= 0; i-- {
		if m.movements[i].UserId == userId {
			list = append(list, m.movements[i])
		}
	}
	if after != nil {
		for i, mv := range list {
			if mv.Id == after.Id {
				list = list[i+1:]
				break
			}
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
	for i, mv := range m.movements {
		if mv.Id == id && mv.UserId == userId {
			m.movements = append(m.movements[:i], m.movements[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

type mockQuotaRepo struct{ counts map[string]int }

func (m *mockQuotaRepo) Reserve(ctx context.Context, userId model.UserId, key string, n, limit int) (bool, error) {
	if m.counts[userId.String()+key]+n > limit {
		return false, nil
	}
	m.counts[userId.String()+key] += n
	return true, nil
}

func (m *mockQuotaRepo) Release(ctx context.Context, userId model.UserId, key string, n int) error {
	m.counts[userId.String()+key] -= n
	return nil
}

// passthroughTx runs fn as it is: the HTTP tests don't fail halfway (the service and Mongo tests do).
type passthroughTx struct{}

func (passthroughTx) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
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
	assets       model.Money
	debts        model.DebtTotals
	byAssetClass []outbound.AssetClassAggregate
	byPlatform   []outbound.PlatformAggregate
}

func (m *mockWealthAggregationPort) Totals(ctx context.Context, userId model.UserId) (outbound.WealthTotals, error) {
	return outbound.WealthTotals{Assets: m.assets, Debts: m.debts.Balance}, nil
}

func (m *mockWealthAggregationPort) Breakdown(ctx context.Context, userId model.UserId) (outbound.WealthBreakdown, error) {
	return outbound.WealthBreakdown{Assets: m.assets, Debts: m.debts, ByAssetClass: m.byAssetClass, ByPlatform: m.byPlatform}, nil
}

// mockDebtRepo keeps debts in memory, isolated by user, listed largest first.
type mockDebtRepo struct {
	debts map[string]model.Debt
}

func newMockDebtRepo() *mockDebtRepo { return &mockDebtRepo{debts: map[string]model.Debt{}} }

func (m *mockDebtRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.Debt, error) {
	var list []model.Debt
	for _, d := range m.debts {
		if d.UserId == userId {
			list = append(list, d)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if c := list[i].Balance.Cmp(list[j].Balance); c != 0 {
			return c > 0
		}
		return list[i].Name < list[j].Name
	})
	return list, nil
}

func (m *mockDebtRepo) FindById(ctx context.Context, userId model.UserId, id model.DebtId) (*model.Debt, error) {
	if d, ok := m.debts[id.String()]; ok && d.UserId == userId {
		return &d, nil
	}
	return nil, nil
}

func (m *mockDebtRepo) Count(ctx context.Context, userId model.UserId) (int64, error) {
	list, _ := m.FindAll(ctx, userId)
	return int64(len(list)), nil
}

func (m *mockDebtRepo) Insert(ctx context.Context, debt model.Debt) error {
	m.debts[debt.Id.String()] = debt
	return nil
}

func (m *mockDebtRepo) Update(ctx context.Context, debt model.Debt) (bool, error) {
	if d, ok := m.debts[debt.Id.String()]; !ok || d.UserId != debt.UserId {
		return false, nil
	}
	m.debts[debt.Id.String()] = debt
	return true, nil
}

func (m *mockDebtRepo) DeleteById(ctx context.Context, userId model.UserId, id model.DebtId) (bool, error) {
	if d, ok := m.debts[id.String()]; !ok || d.UserId != userId {
		return false, nil
	}
	delete(m.debts, id.String())
	return true, nil
}

func (m *mockDebtRepo) ExistingIds(ctx context.Context, userId model.UserId, ids []model.DebtId) (map[model.DebtId]bool, error) {
	existing := map[model.DebtId]bool{}
	for _, id := range ids {
		if d, ok := m.debts[id.String()]; ok && d.UserId == userId {
			existing[id] = true
		}
	}
	return existing, nil
}

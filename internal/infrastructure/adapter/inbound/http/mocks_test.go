package http_test

import (
	"cmp"
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
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

// AssetClassesInUse is assetClasses, if set; otherwise the classes of the user's holdings.
func (m *mockHoldingRepo) AssetClassesInUse(ctx context.Context, userId model.UserId) ([]model.AssetClass, error) {
	if m.assetClasses != nil {
		return m.assetClasses, nil
	}
	var classes []model.AssetClass
	for _, h := range m.sorted(userId) {
		if !slices.Contains(classes, h.AssetClass) {
			classes = append(classes, h.AssetClass)
		}
	}
	return classes, nil
}

// sorted is the user's holdings, oldest first (as the real repository reads them).
func (m *mockHoldingRepo) sorted(userId model.UserId) []model.Holding {
	list, _ := m.FindAll(context.Background(), userId)
	slices.SortFunc(list, func(a, b model.Holding) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.Id.String(), b.Id.String()))
	})
	return list
}

func (m *mockHoldingRepo) ReassignAssetClass(ctx context.Context, userId model.UserId, from, to model.AssetClass) (int, error) {
	moved := 0
	for id, h := range m.holdings {
		if h.UserId == userId && h.AssetClass == from {
			h.AssetClass = to
			m.holdings[id] = h
			moved++
		}
	}
	return moved, nil
}

func (m *mockHoldingRepo) ReassignPlatform(ctx context.Context, userId model.UserId, key string, to model.PlatformName) (int, error) {
	renamed := 0
	for id, h := range m.holdings {
		if h.UserId == userId && model.PlatformKey(h.Platform) == key {
			h.Platform = to
			m.holdings[id] = h
			renamed++
		}
	}
	return renamed, nil
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

func (m *mockHoldingRepo) SetExpectedReturns(ctx context.Context, userId model.UserId, returns map[model.HoldingId]*decimal.Decimal,
	updatedAt time.Time) (int, error) {
	found := 0
	for id, pct := range returns {
		if h, ok := m.holdings[id.String()]; ok && h.UserId == userId {
			h.ExpectedReturnPct, h.UpdatedAt = pct, updatedAt
			m.holdings[id.String()] = h
			found++
		}
	}
	return found, nil
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
	byKey := map[string]int{}
	var list []model.Platform
	for _, h := range m.holdings.sorted(userId) {
		key := model.PlatformKey(h.Platform)
		i, ok := byKey[key]
		if !ok {
			p := model.NewPlatform(userId, h.Platform, model.PlatformTypeOther, h.CreatedAt)
			p.Key, p.Value = key, model.ZeroMoney
			i, byKey[key] = len(list), len(list)
			list = append(list, p)
		}
		list[i].Count++
		list[i].Value = list[i].Value.Plus(h.Value)
	}
	sort.Slice(list, func(i, j int) bool {
		return model.SortName(list[i].Name.Value()) < model.SortName(list[j].Name.Value())
	})
	return list, nil
}

func (m *mockPlatformRepo) Canonical(ctx context.Context, userId model.UserId, name model.PlatformName) (model.PlatformName, error) {
	for _, h := range m.holdings.sorted(userId) {
		if model.PlatformKey(h.Platform) == model.PlatformKey(name) {
			return h.Platform, nil
		}
	}
	return name, nil
}

func (m *mockPlatformRepo) Names(ctx context.Context, userId model.UserId) (map[string]model.PlatformName, error) {
	platforms, _ := m.FindAll(ctx, userId)
	names := map[string]model.PlatformName{}
	for _, p := range platforms {
		names[p.Key] = p.Name
	}
	return names, nil
}

// mockClassSettingsRepo keeps class settings in memory, by user and class.
type mockClassSettingsRepo struct {
	settings map[string]model.AssetClassSettings
}

func newMockClassSettingsRepo() *mockClassSettingsRepo {
	return &mockClassSettingsRepo{settings: map[string]model.AssetClassSettings{}}
}

func (m *mockClassSettingsRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.AssetClassSettings, error) {
	var list []model.AssetClassSettings
	for _, s := range m.settings {
		if s.UserId == userId {
			list = append(list, s)
		}
	}
	return list, nil
}

func (m *mockClassSettingsRepo) Save(ctx context.Context, s model.AssetClassSettings) error {
	m.settings[s.UserId.String()+"/"+s.Name.Value()] = s
	return nil
}

func (m *mockClassSettingsRepo) Delete(ctx context.Context, userId model.UserId, class model.AssetClass) (bool, error) {
	_, ok := m.settings[userId.String()+"/"+class.Value()]
	delete(m.settings, userId.String()+"/"+class.Value())
	return ok, nil
}

// mockPlatformSettingsRepo keeps platform settings in memory, by user and key.
type mockPlatformSettingsRepo struct {
	settings map[string]model.PlatformSettings
}

func newMockPlatformSettingsRepo() *mockPlatformSettingsRepo {
	return &mockPlatformSettingsRepo{settings: map[string]model.PlatformSettings{}}
}

func (m *mockPlatformSettingsRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.PlatformSettings, error) {
	var list []model.PlatformSettings
	for _, s := range m.settings {
		if s.UserId == userId {
			list = append(list, s)
		}
	}
	return list, nil
}

func (m *mockPlatformSettingsRepo) Find(ctx context.Context, userId model.UserId, key string) (*model.PlatformSettings, error) {
	s, ok := m.settings[userId.String()+"/"+key]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (m *mockPlatformSettingsRepo) Save(ctx context.Context, s model.PlatformSettings) error {
	m.settings[s.UserId.String()+"/"+s.Key] = s
	return nil
}

func (m *mockPlatformSettingsRepo) Delete(ctx context.Context, userId model.UserId, key string) (bool, error) {
	_, ok := m.settings[userId.String()+"/"+key]
	delete(m.settings, userId.String()+"/"+key)
	return ok, nil
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
	returns      []model.HoldingReturn
	holdings     *mockHoldingRepo // ByAssetClass totals these, if set
}

func (m *mockWealthAggregationPort) ByAssetClass(ctx context.Context, userId model.UserId) ([]outbound.AssetClassAggregate, error) {
	if m.holdings == nil {
		return m.byAssetClass, nil
	}
	var list []outbound.AssetClassAggregate
	index := map[model.AssetClass]int{}
	for _, h := range m.holdings.sorted(userId) {
		i, ok := index[h.AssetClass]
		if !ok {
			i, index[h.AssetClass] = len(list), len(list)
			list = append(list, outbound.AssetClassAggregate{AssetClass: h.AssetClass, Value: model.ZeroMoney})
		}
		list[i].Value = list[i].Value.Plus(h.Value)
		list[i].Count++
	}
	return list, nil
}

func (m *mockWealthAggregationPort) Totals(ctx context.Context, userId model.UserId) (outbound.WealthTotals, error) {
	return outbound.WealthTotals{Assets: m.assets, Debts: m.debts.Balance, Returns: m.returns}, nil
}

func (m *mockWealthAggregationPort) Breakdown(ctx context.Context, userId model.UserId) (outbound.WealthBreakdown, error) {
	return outbound.WealthBreakdown{Assets: m.assets, Debts: m.debts, ByAssetClass: m.byAssetClass, ByPlatform: m.byPlatform,
		Returns: m.returns}, nil
}

// mockPreferencesRepo keeps each user's preferences in memory.
type mockPreferencesRepo struct {
	saved map[model.UserId]model.Preferences
}

func (m *mockPreferencesRepo) Find(ctx context.Context, userId model.UserId) (*model.Preferences, error) {
	p, ok := m.saved[userId]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (m *mockPreferencesRepo) Save(ctx context.Context, userId model.UserId, preferences model.Preferences) error {
	m.saved[userId] = preferences
	return nil
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

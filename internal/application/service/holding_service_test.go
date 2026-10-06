package service_test

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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
	beforeDelete  func() // lands a "concurrent" delete between the service's read and its own
	beforeSave    func() // lands a "concurrent" request between the service's checks and its insert
	findErr       error
	updateErr     error
	beforeUpdate  func() // lands a "concurrent" request between the service's read and its update
	updates       int
	existingErr   error
	setReturnsErr error
	beforeReturns func() // lands a "concurrent" request between the service's read and its writes
	returnWrites  int
	reassignErr   error
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
	if m.findErr != nil {
		return nil, m.findErr
	}
	h, ok := m.holdings[id.String()]
	if !ok || h.UserId != userId {
		return nil, nil
	}
	return &h, nil
}

func (m *mockHoldingRepo) Update(ctx context.Context, holding model.Holding) (bool, error) {
	if m.beforeUpdate != nil {
		m.beforeUpdate()
	}
	if m.updateErr != nil {
		return false, m.updateErr
	}
	h, ok := m.holdings[holding.Id.String()]
	if !ok || h.UserId != holding.UserId {
		return false, nil
	}
	m.holdings[holding.Id.String()] = holding
	m.updates++
	return true, nil
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
	if m.beforeDelete != nil {
		m.beforeDelete()
	}
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
	if m.reassignErr != nil {
		return 0, m.reassignErr
	}
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
	if m.reassignErr != nil {
		return 0, m.reassignErr
	}
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
	if m.existingErr != nil {
		return nil, m.existingErr
	}
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
	if m.beforeReturns != nil {
		m.beforeReturns()
	}
	if m.setReturnsErr != nil {
		return 0, m.setReturnsErr
	}
	found := 0
	for id, pct := range returns {
		h, ok := m.holdings[id.String()]
		if !ok || h.UserId != userId {
			continue
		}
		h.ExpectedReturnPct, h.UpdatedAt = pct, updatedAt
		m.holdings[id.String()] = h
		found++
	}
	m.returnWrites++
	return found, nil
}

// mockPlatformRepo derives platforms from the holdings, like the real one.
type mockPlatformRepo struct {
	holdings     *mockHoldingRepo
	canonicalErr error
}

func newMockPlatformRepo(holdings *mockHoldingRepo) *mockPlatformRepo {
	return &mockPlatformRepo{holdings: holdings}
}

func (m *mockPlatformRepo) FindAll(ctx context.Context, userId model.UserId) ([]model.Platform, error) {
	byKey := map[string]*model.Platform{}
	var list []*model.Platform
	for _, h := range m.holdings.sorted(userId) {
		key := model.PlatformKey(h.Platform)
		p, ok := byKey[key]
		if !ok {
			created := model.NewPlatform(userId, h.Platform, model.PlatformTypeOther, h.CreatedAt)
			created.Key, created.Value = key, model.ZeroMoney
			p = &created
			byKey[key] = p
			list = append(list, p)
		}
		p.Count++
		p.Value = p.Value.Plus(h.Value)
	}
	platforms := make([]model.Platform, len(list))
	for i, p := range list {
		platforms[i] = *p
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i].Name.Value() < platforms[j].Name.Value() })
	return platforms, nil
}

func (m *mockPlatformRepo) Names(ctx context.Context, userId model.UserId) (map[string]model.PlatformName, error) {
	platforms, _ := m.FindAll(ctx, userId)
	names := map[string]model.PlatformName{}
	for _, p := range platforms {
		names[p.Key] = p.Name
	}
	return names, nil
}

func (m *mockPlatformRepo) Canonical(ctx context.Context, userId model.UserId, name model.PlatformName) (model.PlatformName, error) {
	if m.canonicalErr != nil {
		return model.PlatformName{}, m.canonicalErr
	}
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
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	f := newLedgerFixture(fixedClock(now))
	svc := f.holdingSvc
	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	create := func(name, platform string) model.Holding {
		h, err := svc.CreateHolding(ctx, inbound.CreateHoldingCommand{
			UserId: userId, Name: name, AssetClass: "Equity", Platform: platform, ValueUsd: 10557.00,
		})
		require.NoError(t, err)
		return h
	}

	// 1. Create reuses an existing platform case-insensitively, else auto-creates it, and records an OPENING.
	nvda := create("NVDA", "Balanz")
	assert.Equal(t, "NVDA", nvda.Name)
	assert.Equal(t, "10557.00", nvda.Value.String())
	aapl := create("AAPL", "balanz")
	assert.Equal(t, "Balanz", aapl.Platform.Value())
	platforms := func() []model.Platform {
		list, err := f.platforms.FindAll(ctx, userId)
		require.NoError(t, err)
		return list
	}
	require.Len(t, platforms(), 1)
	assert.Equal(t, "Other", platforms()[0].Type.Value())
	require.Len(t, f.movements.movements, 2)
	opening := f.movements.movements[0]
	assert.Equal(t, model.MovementOpening, opening.Kind)
	assert.Equal(t, nvda.Id, opening.Holding.Id)
	assert.Equal(t, "10557.00", opening.Amount.String())
	assert.Equal(t, now, opening.OccurredAt)
	assert.Equal(t, 2, f.quotas.movementsOf(userId))

	all, err := svc.GetAllHoldings(ctx, userId)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// 2. Deleting one holding keeps the platform while another holding still uses it, and records its CLOSING
	// (not counted against the quota: removing always works).
	require.NoError(t, svc.DeleteHolding(ctx, userId, nvda.Id))
	assert.Len(t, platforms(), 1)
	closing := f.movements.movements[2]
	assert.Equal(t, model.MovementClosing, closing.Kind)
	assert.Equal(t, "NVDA", closing.Holding.Name)
	assert.Equal(t, "10557.00", closing.Amount.String())
	assert.Equal(t, 2, f.quotas.movementsOf(userId))

	// 3. Deleting the last one prunes the platform (the UI has no way to delete it).
	require.NoError(t, svc.DeleteHolding(ctx, userId, aapl.Id))
	assert.Empty(t, platforms())

	// 4. Delete again -> not found, and nothing recorded.
	assert.ErrorAs(t, svc.DeleteHolding(ctx, userId, aapl.Id), &appErrors.ResourceNotFoundError{})
	assert.Len(t, f.movements.movements, 4)
}

func TestHoldingService_RejectedCreateLeavesNothingBehind(t *testing.T) {
	f := newLedgerFixture(fixedClock(time.Now()))
	userId := model.NewUserId(uuid.New())

	for name, cmd := range map[string]inbound.CreateHoldingCommand{
		"name too long":        {Name: strings.Repeat("x", model.MaxHoldingNameLength+1), AssetClass: "Cash", Platform: "Brand New", ValueUsd: 1},
		"asset class too long": {Name: "x", AssetClass: strings.Repeat("x", model.MaxAssetClassLength+1), Platform: "Brand New", ValueUsd: 1},
		"negative value":       {Name: "x", AssetClass: "Cash", Platform: "Brand New", ValueUsd: -1},
		"blank platform":       {Name: "x", AssetClass: "Cash", Platform: "  ", ValueUsd: 1},
	} {
		t.Run(name, func(t *testing.T) {
			cmd.UserId = userId
			_, err := f.holdingSvc.CreateHolding(context.Background(), cmd)
			assert.Error(t, err)
			assert.Empty(t, f.holdings.holdings)
			assert.Empty(t, f.movements.movements)
			assert.Zero(t, f.tx.calls, "validated before any transaction")
		})
	}
}

func TestHoldingService_CapsHoldingsPerUser(t *testing.T) {
	f := newLedgerFixture(fixedClock(time.Now()))
	full, other := model.NewUserId(uuid.New()), model.NewUserId(uuid.New())

	for i := 0; i < model.MaxHoldingsPerUser; i++ {
		h := model.Holding{Id: model.NewHoldingId(), UserId: full, Platform: model.MustPlatformName("Bank")}
		f.holdings.holdings[h.Id.String()] = h
	}
	cmd := func(u model.UserId) inbound.CreateHoldingCommand {
		return inbound.CreateHoldingCommand{UserId: u, Name: "One more", AssetClass: "Cash", Platform: "New Bank", ValueUsd: 1}
	}

	_, err := f.holdingSvc.CreateHolding(context.Background(), cmd(full))
	assert.ErrorAs(t, err, &appErrors.LimitExceededError{})
	platforms, _ := f.platforms.FindAll(context.Background(), full)
	require.Len(t, platforms, 1)
	assert.Equal(t, "Bank", platforms[0].Name.Value(), "no platform for a refused holding")
	assert.Empty(t, f.movements.movements)
	assert.Zero(t, f.quotas.movementsOf(full))

	// The cap is per account: everyone else is unaffected.
	_, err = f.holdingSvc.CreateHolding(context.Background(), cmd(other))
	assert.NoError(t, err)

	f.holdings.countErr = errors.New("db down")
	_, err = f.holdingSvc.CreateHolding(context.Background(), cmd(other))
	assert.EqualError(t, err, "db down")
}

func TestHoldingService_CreateIsAllOrNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		breakIt func(f *ledgerFixture, user model.UserId)
		want    func(t *testing.T, err error)
	}{
		"the movements quota is full": {
			breakIt: func(f *ledgerFixture, user model.UserId) {
				f.quotas.counts[user.String()+"/movements"] = model.MaxMovementsPerUser
			},
			want: func(t *testing.T, err error) {
				assert.ErrorAs(t, err, &appErrors.LimitExceededError{})
				assert.ErrorContains(t, err, "20000 recorded changes")
			},
		},
		"the OPENING can't be stored": {
			breakIt: func(f *ledgerFixture, _ model.UserId) { f.movements.saveErr = errors.New("insert failed") },
			want:    func(t *testing.T, err error) { assert.EqualError(t, err, "insert failed") },
		},
		"the quota can't be read": {
			breakIt: func(f *ledgerFixture, _ model.UserId) { f.quotas.reserveErr = errors.New("quota down") },
			want:    func(t *testing.T, err error) { assert.EqualError(t, err, "quota down") },
		},
		"the platform can't be read": {
			breakIt: func(f *ledgerFixture, _ model.UserId) { f.platforms.canonicalErr = errors.New("canonical failed") },
			want:    func(t *testing.T, err error) { assert.EqualError(t, err, "canonical failed") },
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newLedgerFixture(fixedClock(time.Now()))
			user := model.NewUserId(uuid.New())
			tc.breakIt(f, user)

			_, err := f.holdingSvc.CreateHolding(context.Background(), inbound.CreateHoldingCommand{
				UserId: user, Name: "BTC", AssetClass: "Crypto", Platform: "Ledger", ValueUsd: 1,
			})

			tc.want(t, err)
			assert.Empty(t, f.holdings.holdings, "the holding was rolled back with the rest")
			assert.Empty(t, f.movements.movements)
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestHoldingService_UpdateChangesOnlyWhatIsSent(t *testing.T) {
	created := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	now := created
	f := newLedgerFixture(func() time.Time { return now })
	svc := f.holdingSvc
	ctx := context.Background()
	user := model.NewUserId(uuid.New())
	create := func(name, platform string) model.Holding {
		h, err := svc.CreateHolding(ctx, inbound.CreateHoldingCommand{UserId: user, Name: name, AssetClass: "Crypto", Platform: platform, ValueUsd: 100})
		require.NoError(t, err)
		return h
	}
	btc := create("BTC", "Ledger")
	create("ETH", "Binance")
	now = created.Add(48 * time.Hour)

	got, err := svc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: user, Id: btc.Id, ValueUsd: ptr(20000.005)})
	require.NoError(t, err)
	assert.Equal(t, "20000.01", got.Value.String())
	assert.Equal(t, "BTC", got.Name)
	assert.Equal(t, "Crypto", got.AssetClass.Value())
	assert.Equal(t, "Ledger", got.Platform.Value())
	assert.Equal(t, created, got.CreatedAt)
	assert.Equal(t, now, got.UpdatedAt)
	assert.Equal(t, got, f.holdings.holdings[btc.Id.String()], "what's returned is what's stored")

	// Moving it to a platform the user has under another case keeps that spelling, and the platform it
	// left (its only holding) is gone. No value change, no movement.
	recorded := len(f.movements.movements)
	got, err = svc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{
		UserId: user, Id: btc.Id, Name: ptr("  Bitcoin   (cold) "), AssetClass: ptr("Store of value"), Platform: ptr("binance"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Bitcoin (cold)", got.Name)
	assert.Equal(t, "Store of value", got.AssetClass.Value())
	assert.Equal(t, "Binance", got.Platform.Value())
	platforms, _ := f.platforms.FindAll(ctx, user)
	require.Len(t, platforms, 1)
	assert.Equal(t, "Binance", platforms[0].Name.Value())
	assert.Len(t, f.movements.movements, recorded)
}

func TestHoldingService_UpdateRecordsWhyTheValueChanged(t *testing.T) {
	now := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	f := newLedgerFixture(fixedClock(now))
	ctx := context.Background()
	user := model.NewUserId(uuid.New())
	h, err := f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{UserId: user, Name: "Savings", AssetClass: "Cash", Platform: "Bank", ValueUsd: 100})
	require.NoError(t, err)
	lastMovement := func() model.Movement { return f.movements.movements[len(f.movements.movements)-1] }
	yesterday := now.Add(-24 * time.Hour)

	for _, tc := range []struct {
		reason string
		value  float64
		kind   model.MovementKind
		amount string
	}{
		{"", 150, model.MovementGain, "50.00"},
		{"MARKET", 120, model.MovementLoss, "30.00"},
		{"CASH_FLOW", 220, model.MovementDeposit, "100.00"},
		{"CASH_FLOW", 200, model.MovementWithdrawal, "20.00"},
		{"CORRECTION", 210.5, model.MovementAdjustment, "10.50"},
	} {
		before := f.holdings.holdings[h.Id.String()].Value
		_, err := f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{
			UserId: user, Id: h.Id, ValueUsd: ptr(tc.value), ValueChangeReason: tc.reason, OccurredAt: &yesterday, Note: "  from the statement ",
		})
		require.NoError(t, err)
		m := lastMovement()
		assert.Equal(t, tc.kind, m.Kind, tc.reason)
		assert.Equal(t, tc.amount, m.Amount.String())
		assert.Equal(t, before.String(), m.PreviousValue.String())
		assert.Equal(t, model.MustMoneyFromFloat(tc.value).String(), m.NewValue.String())
		assert.Equal(t, yesterday, m.OccurredAt)
		assert.Equal(t, now, m.CreatedAt)
		assert.Equal(t, "from the statement", m.Note)
		assert.Equal(t, h.Id, m.Holding.Id)
	}

	// The change is measured against the value read in the transaction, not one sent with the edit.
	f.holdings.holdings[h.Id.String()] = func() model.Holding {
		changed := f.holdings.holdings[h.Id.String()]
		changed.Value = model.MustMoneyFromFloat(300) // edited from another device meanwhile
		return changed
	}()
	_, err = f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: user, Id: h.Id, ValueUsd: ptr(250.0)})
	require.NoError(t, err)
	assert.Equal(t, model.MovementLoss, lastMovement().Kind)
	assert.Equal(t, "50.00", lastMovement().Amount.String())
}

func TestHoldingService_UpdateWithNothingNewWritesNothing(t *testing.T) {
	created := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	now := created
	f := newLedgerFixture(func() time.Time { return now })
	user := model.NewUserId(uuid.New())
	h, err := f.holdingSvc.CreateHolding(context.Background(), inbound.CreateHoldingCommand{UserId: user, Name: "BTC", AssetClass: "Crypto", Platform: "Ledger", ValueUsd: 100})
	require.NoError(t, err)
	now = created.Add(time.Hour)

	for name, cmd := range map[string]inbound.UpdateHoldingCommand{
		"empty":       {},
		"same values": {Name: ptr(" BTC "), AssetClass: ptr("Crypto"), Platform: ptr("ledger"), ValueUsd: ptr(100.004)},
	} {
		t.Run(name, func(t *testing.T) {
			cmd.UserId, cmd.Id = user, h.Id
			got, err := f.holdingSvc.UpdateHolding(context.Background(), cmd)
			require.NoError(t, err)
			assert.Equal(t, created, got.UpdatedAt)
			assert.Zero(t, f.holdings.updates)
			assert.Len(t, f.movements.movements, 1, "just the OPENING")
		})
	}
}

func TestHoldingService_UpdateValidatesBeforeReading(t *testing.T) {
	f := newLedgerFixture(fixedClock(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)))
	f.holdings.findErr = errors.New("must not be read")
	long := strings.Repeat("x", model.MaxPlatformNameLength+1)
	tomorrowAndABit := time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		cmd  inbound.UpdateHoldingCommand
		want error
	}{
		"name too long":  {inbound.UpdateHoldingCommand{Name: ptr(long)}, model.ErrLabelTooLong},
		"blank class":    {inbound.UpdateHoldingCommand{AssetClass: ptr(" ")}, model.ErrBlankLabel},
		"blank platform": {inbound.UpdateHoldingCommand{Platform: ptr("")}, model.ErrBlankLabel},
		"negative value": {inbound.UpdateHoldingCommand{ValueUsd: ptr(-1.0)}, model.ErrNegativeMoney},
		"NaN value":      {inbound.UpdateHoldingCommand{ValueUsd: ptr(math.NaN())}, model.ErrNonFiniteMoney},
		"unknown reason": {inbound.UpdateHoldingCommand{ValueUsd: ptr(1.0), ValueChangeReason: "TAXES"}, model.ErrUnknownValueChangeReason},
		"note too long":  {inbound.UpdateHoldingCommand{ValueUsd: ptr(1.0), Note: strings.Repeat("n", model.MaxMovementNoteLength+1)}, model.ErrLabelTooLong},
		"in the future":  {inbound.UpdateHoldingCommand{ValueUsd: ptr(1.0), OccurredAt: &tomorrowAndABit}, model.ErrOccurredAtOutOfRange},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.holdingSvc.UpdateHolding(context.Background(), tc.cmd)
			assert.ErrorIs(t, err, tc.want)
			assert.Zero(t, f.tx.calls)
		})
	}
}

func TestHoldingService_UpdateOfAMissingHoldingIsNotFound(t *testing.T) {
	f := newLedgerFixture(fixedClock(time.Now()))
	ctx := context.Background()
	owner, other := model.NewUserId(uuid.New()), model.NewUserId(uuid.New())
	h, err := f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{UserId: owner, Name: "BTC", AssetClass: "Crypto", Platform: "Ledger", ValueUsd: 100})
	require.NoError(t, err)

	// Someone else's holding is as missing as one that never was.
	for _, cmd := range []inbound.UpdateHoldingCommand{
		{UserId: other, Id: h.Id, Name: ptr("Mine now")},
		{UserId: owner, Id: model.NewHoldingId(), Name: ptr("Ghost")},
	} {
		_, err := f.holdingSvc.UpdateHolding(ctx, cmd)
		assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
	}
	assert.Equal(t, "BTC", f.holdings.holdings[h.Id.String()].Name)

	// Deleted (from another device) between the read and the write: not found, not brought back, and the
	// movement isn't recorded either.
	f.holdings.beforeUpdate = func() { delete(f.holdings.holdings, h.Id.String()) }
	_, err = f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: owner, Id: h.Id, Name: ptr("Edited"), ValueUsd: ptr(1.0)})
	assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
	assert.Len(t, f.movements.movements, 1, "just the OPENING")
}

func TestHoldingService_UpdateReturnsStorageErrors(t *testing.T) {
	f := newLedgerFixture(fixedClock(time.Now()))
	ctx := context.Background()
	user := model.NewUserId(uuid.New())
	h, err := f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{UserId: user, Name: "BTC", AssetClass: "Crypto", Platform: "Ledger", ValueUsd: 100})
	require.NoError(t, err)
	update := func() error {
		_, err := f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: user, Id: h.Id, Platform: ptr("Binance"), ValueUsd: ptr(50.0)})
		return err
	}

	f.holdings.findErr = errors.New("find failed")
	assert.EqualError(t, update(), "find failed")
	f.holdings.findErr = nil

	f.platforms.canonicalErr = errors.New("canonical failed")
	assert.EqualError(t, update(), "canonical failed")
	f.platforms.canonicalErr = nil

	f.holdings.updateErr = errors.New("update failed")
	assert.EqualError(t, update(), "update failed")
	f.holdings.updateErr = nil

	// The movement fails: the new value is rolled back with it.
	f.movements.saveErr = errors.New("insert failed")
	assert.EqualError(t, update(), "insert failed")
	assert.Equal(t, "100.00", f.holdings.holdings[h.Id.String()].Value.String())
	assert.Equal(t, "Ledger", f.holdings.holdings[h.Id.String()].Platform.Value())
}

func TestHoldingService_DeleteIsAllOrNothing(t *testing.T) {
	f := newLedgerFixture(fixedClock(time.Now()))
	ctx := context.Background()
	user := model.NewUserId(uuid.New())
	h, err := f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{UserId: user, Name: "BTC", AssetClass: "Crypto", Platform: "Ledger", ValueUsd: 100})
	require.NoError(t, err)

	f.movements.saveErr = errors.New("insert failed")
	assert.EqualError(t, f.holdingSvc.DeleteHolding(ctx, user, h.Id), "insert failed")
	assert.Contains(t, f.holdings.holdings, h.Id.String(), "the holding is back: its CLOSING couldn't be stored")
	f.movements.saveErr = nil

	f.holdings.deleteErr = errors.New("delete failed")
	assert.EqualError(t, f.holdingSvc.DeleteHolding(ctx, user, h.Id), "delete failed")
	f.holdings.deleteErr = nil

	f.holdings.findErr = errors.New("find failed")
	assert.EqualError(t, f.holdingSvc.DeleteHolding(ctx, user, h.Id), "find failed")
	f.holdings.findErr = nil

	// Gone between the read and the delete (another device): not found.
	f.holdings.beforeDelete = func() { delete(f.holdings.holdings, h.Id.String()) }
	assert.ErrorAs(t, f.holdingSvc.DeleteHolding(ctx, user, h.Id), &appErrors.ResourceNotFoundError{})
}

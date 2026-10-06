package model_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIds_RoundTrip(t *testing.T) {
	u := uuid.New()

	userId, err := model.ParseUserId(u.String())
	require.NoError(t, err)
	assert.Equal(t, u, userId.UUID())
	assert.Equal(t, u.String(), userId.String())
	assert.Equal(t, userId, model.NewUserId(u))
	assert.Equal(t, userId, model.MustParseUserId(u.String()))

	holdingId, err := model.ParseHoldingId(u.String())
	require.NoError(t, err)
	assert.Equal(t, u, holdingId.UUID())
	assert.Equal(t, u.String(), holdingId.String())
	assert.Equal(t, holdingId, model.HoldingIdFromUUID(u))

	snapshotId, err := model.ParseSnapshotId(u.String())
	require.NoError(t, err)
	assert.Equal(t, u, snapshotId.UUID())
	assert.Equal(t, u.String(), snapshotId.String())
	assert.Equal(t, snapshotId, model.SnapshotIdFromUUID(u))
}

func TestIds_NewAreUnique(t *testing.T) {
	assert.NotEqual(t, model.NewHoldingId(), model.NewHoldingId())
	assert.NotEqual(t, model.NewSnapshotId(), model.NewSnapshotId())
}

func TestIds_RejectInvalid(t *testing.T) {
	_, err := model.ParseUserId("nope")
	assert.ErrorIs(t, err, model.ErrInvalidUUID)
	_, err = model.ParseHoldingId("nope")
	assert.ErrorIs(t, err, model.ErrInvalidUUID)
	_, err = model.ParseSnapshotId("nope")
	assert.ErrorIs(t, err, model.ErrInvalidUUID)
	assert.Panics(t, func() { model.MustParseUserId("nope") })
}

func TestPlatformType(t *testing.T) {
	pt, err := model.NewPlatformType("")
	require.NoError(t, err)
	assert.Equal(t, model.PlatformTypeOther, pt)

	pt = model.MustPlatformType("  Broker  ")
	assert.Equal(t, "Broker", pt.Value())
	assert.Equal(t, "Broker", pt.String())

	_, err = model.NewPlatformType("   ")
	assert.ErrorIs(t, err, model.ErrBlankLabel)
	assert.Panics(t, func() { model.MustPlatformType(strings.Repeat("x", model.MaxPlatformTypeLength+1)) })
}

func TestPlatformName(t *testing.T) {
	pn := model.MustPlatformName(" Interactive   Brokers ")
	assert.Equal(t, "Interactive Brokers", pn.Value())
	assert.Equal(t, "Interactive Brokers", pn.String())

	_, err := model.NewPlatformName("")
	assert.ErrorIs(t, err, model.ErrBlankLabel)
	assert.Panics(t, func() { model.MustPlatformName("") })
}

func TestAssetClass(t *testing.T) {
	ac := model.MustAssetClass("Equity")
	assert.Equal(t, "Equity", ac.String())
	assert.Panics(t, func() { model.MustAssetClass("") })
}

func TestNewPlatform(t *testing.T) {
	user := model.NewUserId(uuid.New())
	now := time.Now()
	p := model.NewPlatform(user, model.MustPlatformName("Binance"), model.PlatformTypeOther, now)

	assert.Equal(t, model.Platform{UserId: user, Name: model.MustPlatformName("Binance"), Type: model.PlatformTypeOther, CreatedAt: now}, p)
}

func TestCreateHolding(t *testing.T) {
	user := model.NewUserId(uuid.New())
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := model.MustMoneyFromFloat(10)

	h, err := model.CreateHolding(user, "  My   ETF ", model.MustAssetClass("Equity"), model.MustPlatformName("IBKR"), value, now)

	require.NoError(t, err)
	assert.Equal(t, "My ETF", h.Name)
	assert.Equal(t, user, h.UserId)
	assert.Equal(t, now, h.CreatedAt)
	assert.Equal(t, now, h.UpdatedAt)
	assert.NotEqual(t, uuid.Nil, h.Id.UUID())

	_, err = model.CreateHolding(user, " ", model.MustAssetClass("Equity"), model.MustPlatformName("IBKR"), value, now)
	assert.ErrorIs(t, err, model.ErrBlankLabel)
}

func TestNewNetWorthSnapshot(t *testing.T) {
	id := model.NewSnapshotId()
	user := model.NewUserId(uuid.New())
	at := time.Now()
	assets, debts := model.MustMoneyFromFloat(5), model.MustMoneyFromFloat(7.5)

	s := model.NewNetWorthSnapshot(id, user, at, assets, debts)

	assert.Equal(t, model.NetWorthSnapshot{Id: id, UserId: user, CapturedAt: at, Assets: assets, Debts: debts, TotalValue: model.NetOf(assets, debts),
		Source: model.SnapshotAuto}, s)
	assert.Equal(t, "-2.50", s.TotalValue.String(), "the net worth: below zero when more is owed")
}

func TestYtdGrowth(t *testing.T) {
	at := time.Now()
	g := model.NewYtdGrowthFrom(model.YtdBasisYearStartSnapshot, model.MustMoneyFromFloat(100).Signed(), at, decimal.NewFromInt(5))
	assert.Equal(t, model.YtdBasisYearStartSnapshot, g.Basis)
	assert.Equal(t, "100.00", g.BaselineValue.String())
	assert.Equal(t, at, *g.BaselineAt)

	none := model.NewYtdGrowthNoBaseline()
	assert.Equal(t, model.YtdBasisNoBaseline, none.Basis)
	assert.Nil(t, none.BaselineValue)
	assert.Nil(t, none.BaselineAt)
	assert.True(t, none.GrowthPct.IsZero())
}

func TestMoney_Accessors(t *testing.T) {
	m := model.MustMoneyFromFloat(12.345)
	assert.Equal(t, "12.35", m.Amount().StringFixed(2))
	assert.Equal(t, 12.35, m.Float64())
	assert.True(t, m.GreaterThanOrEqual(m))
	assert.False(t, model.ZeroMoney.GreaterThanOrEqual(m))
}

func TestMoney_Times(t *testing.T) {
	m := model.MustMoneyFromFloat(10)
	assert.Equal(t, "15.00", m.Times(decimal.RequireFromString("1.5")).String())
	assert.Equal(t, model.ZeroMoney, m.Times(decimal.NewFromInt(-1)))
}

func TestMoney_MustPanicsOnNegative(t *testing.T) {
	assert.Panics(t, func() { model.MustMoney(decimal.NewFromInt(-1)) })
	assert.Panics(t, func() { model.MustMoneyFromFloat(-1) })
}

func TestMoney_MinusAndSum(t *testing.T) {
	assert.Equal(t, "5.00", model.MustMoneyFromFloat(10).Minus(model.MustMoneyFromFloat(5)).String())
	assert.Equal(t, "3.00", model.SumMoney([]model.Money{model.MustMoneyFromFloat(1), model.MustMoneyFromFloat(2)}).String())
}

func TestNewExpectedReturnPct(t *testing.T) {
	for v, want := range map[float64]string{7.5: "7.5", -100: "-100", 100: "100", 0: "0", 3.14159: "3.14"} {
		pct, err := model.NewExpectedReturnPct(v)
		require.NoError(t, err, v)
		assert.Equal(t, want, pct.String(), v)
	}
	for _, v := range []float64{-100.01, 100.01, math.NaN(), math.Inf(1)} {
		_, err := model.NewExpectedReturnPct(v)
		assert.ErrorIs(t, err, model.ErrExpectedReturnOutOfRange, v)
	}
	assert.EqualError(t, model.ErrExpectedReturnOutOfRange, "expectedReturnPct must be between -100 and 100")
}

func TestHolding_EffectiveReturnIsItsOwn(t *testing.T) {
	seven := decimal.NewFromInt(7)
	h := model.Holding{ExpectedReturnPct: &seven}
	assert.Equal(t, &seven, h.EffectiveReturnPct())
	assert.Nil(t, model.Holding{}.EffectiveReturnPct())

	other := decimal.RequireFromString("7.00")
	assert.True(t, model.SameReturn(&seven, &other))
	assert.True(t, model.SameReturn(nil, nil))
	assert.False(t, model.SameReturn(&seven, nil))
	assert.False(t, model.SameReturn(nil, &seven))
	eight := decimal.NewFromInt(8)
	assert.False(t, model.SameReturn(&seven, &eight))
}

func TestPreferences_DefaultsAndChecks(t *testing.T) {
	defaults := model.DefaultPreferences().Estimate
	assert.Equal(t, "900.00", defaults.Contribution.String())
	assert.Equal(t, 12, defaults.Years)
	assert.Equal(t, model.YieldModePortfolio, defaults.YieldMode)
	assert.Equal(t, "9", defaults.CustomYieldPct.String())
	assert.Equal(t, []string{"150000.00", "250000.00"}, []string{defaults.Milestones[0].String(), defaults.Milestones[1].String()})
	checked, err := defaults.Check()
	require.NoError(t, err)
	assert.Equal(t, defaults, checked)

	// Milestones come back in order.
	e := defaults
	e.Milestones = []model.Money{model.MustMoneyFromFloat(3), model.MustMoneyFromFloat(1)}
	checked, err = e.Check()
	require.NoError(t, err)
	assert.Equal(t, "1.00", checked.Milestones[0].String())

	bad := func(change func(*model.EstimatePreferences)) error {
		e := defaults
		change(&e)
		_, err := e.Check()
		return err
	}
	assert.ErrorIs(t, bad(func(e *model.EstimatePreferences) { e.Contribution = model.MustMoneyFromFloat(1e9 + 1) }), model.ErrContributionOutOfRange)
	assert.ErrorIs(t, bad(func(e *model.EstimatePreferences) { e.Years = 51 }), model.ErrYearsOutOfRange)
	assert.ErrorIs(t, bad(func(e *model.EstimatePreferences) { e.YieldMode = "MAGIC" }), model.ErrUnknownYieldMode)
	assert.ErrorIs(t, bad(func(e *model.EstimatePreferences) { e.CustomYieldPct = decimal.NewFromInt(-101) }), model.ErrCustomYieldOutOfRange)
	assert.ErrorIs(t, bad(func(e *model.EstimatePreferences) { e.Milestones = make([]model.Money, 6) }), model.ErrTooManyMilestones)
	assert.ErrorIs(t, bad(func(e *model.EstimatePreferences) { e.Milestones = []model.Money{model.MustMoneyFromFloat(1e15 + 1)} }),
		model.ErrMilestoneOutOfRange)
	assert.ErrorIs(t, bad(func(e *model.EstimatePreferences) { e.InflationPct = decimal.NewFromInt(51) }), model.ErrInflationOutOfRange)
	assert.ErrorIs(t, bad(func(e *model.EstimatePreferences) { e.ContributionGrowthPct = decimal.NewFromInt(-1) }),
		model.ErrContributionGrowthOutOfRange)
}

func TestParseYieldModeAndMilestones(t *testing.T) {
	mode, err := model.ParseYieldMode("CUSTOM")
	require.NoError(t, err)
	assert.Equal(t, model.YieldModeCustom, mode)
	_, err = model.ParseYieldMode("custom")
	assert.EqualError(t, err, `yieldMode must be one of PORTFOLIO, CUSTOM (got "custom")`)

	m, err := model.NewMilestone(150000.004)
	require.NoError(t, err)
	assert.Equal(t, "150000.00", m.String())
	for _, v := range []float64{-1, 1e15 + 1, math.NaN()} {
		_, err := model.NewMilestone(v)
		assert.ErrorIs(t, err, model.ErrMilestoneOutOfRange, v)
	}
}

func TestNewManualSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	user := model.NewUserId(uuid.New())
	at := time.Date(2025, 12, 31, 15, 4, 5, 999, time.FixedZone("ART", -3*3600))
	assets, debts := model.MustMoneyFromFloat(95000), model.MustMoneyFromFloat(14000)

	s, err := model.NewManualSnapshot(model.NewSnapshotId(), user, at, decimal.NewFromInt(81000), &assets, &debts, "  note ", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2025, 12, 31, 18, 4, 5, 0, time.UTC), s.CapturedAt, "UTC, to the second")
	assert.Equal(t, model.SnapshotManual, s.Source)
	assert.Equal(t, "note", s.Note)

	positive, err := model.NewManualSnapshot(model.NewSnapshotId(), user, at, decimal.RequireFromString("10.005"), nil, nil, "", now)
	require.NoError(t, err)
	assert.Equal(t, "10.01", positive.Assets.String())
	assert.Equal(t, "0.00", positive.Debts.String())
	negative, err := model.NewManualSnapshot(model.NewSnapshotId(), user, at, decimal.NewFromInt(-5), nil, nil, "", now)
	require.NoError(t, err)
	assert.Equal(t, "5.00", negative.Debts.String())

	for _, tc := range []struct {
		at            time.Time
		assets, debts *model.Money
		total         int64
		note          string
		want          error
	}{
		{at: now.Add(time.Second), want: model.ErrCapturedAtOutOfRange},
		{at: time.Date(1969, 12, 31, 23, 59, 59, 0, time.UTC), want: model.ErrCapturedAtOutOfRange},
		{at: at, assets: &assets, want: model.ErrSnapshotPartsIncomplete},
		{at: at, debts: &debts, want: model.ErrSnapshotPartsIncomplete},
		{at: at, assets: &assets, debts: &debts, total: 1, want: model.ErrSnapshotPartsMismatch},
		{at: at, note: strings.Repeat("x", 201), want: model.ErrLabelTooLong},
	} {
		_, err := model.NewManualSnapshot(model.NewSnapshotId(), user, tc.at, decimal.NewFromInt(tc.total), tc.assets, tc.debts, tc.note, now)
		assert.ErrorIs(t, err, tc.want)
	}
	// Exactly now is fine.
	_, err = model.NewManualSnapshot(model.NewSnapshotId(), user, now, decimal.Zero, nil, nil, "", now)
	assert.NoError(t, err)
}

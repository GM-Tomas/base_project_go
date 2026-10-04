package model_test

import (
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
	total := model.MustMoneyFromFloat(5)

	s := model.NewNetWorthSnapshot(id, user, at, total)

	assert.Equal(t, model.NetWorthSnapshot{Id: id, UserId: user, CapturedAt: at, TotalValue: total}, s)
}

func TestYtdGrowth(t *testing.T) {
	at := time.Now()
	g := model.NewYtdGrowthFrom(model.YtdBasisYearStartSnapshot, model.MustMoneyFromFloat(100), at, decimal.NewFromInt(5))
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

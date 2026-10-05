package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// returnOf is a holding's expected return as text, "" without one.
func returnOf(h model.Holding) string {
	if h.ExpectedReturnPct == nil {
		return ""
	}
	return h.ExpectedReturnPct.String()
}

func TestHoldingService_ExpectedReturnOnCreateAndEdit(t *testing.T) {
	created := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	now := created
	f := newLedgerFixture(func() time.Time { return now })
	ctx := context.Background()
	user := model.NewUserId(uuid.New())

	voo, err := f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{
		UserId: user, Name: "VOO", AssetClass: "Index Fund", Platform: "IBKR", ValueUsd: 1000, ExpectedReturnPct: ptr(7.456),
	})
	require.NoError(t, err)
	assert.Equal(t, "7.46", returnOf(voo), "kept to 2 decimals")
	assert.Equal(t, "7.46", returnOf(f.holdings.holdings[voo.Id.String()]))

	_, err = f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{
		UserId: user, Name: "x", AssetClass: "Cash", Platform: "Bank", ValueUsd: 1, ExpectedReturnPct: ptr(-100.5),
	})
	assert.ErrorIs(t, err, model.ErrExpectedReturnOutOfRange)

	// A new return alone is a change (no movement); the same one, or none sent, writes nothing.
	now = created.Add(time.Hour)
	edited, err := f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{
		UserId: user, Id: voo.Id, ExpectedReturnPct: inbound.Change[float64]{Set: true, Value: ptr(8.0)},
	})
	require.NoError(t, err)
	assert.Equal(t, "8", returnOf(edited))
	assert.Equal(t, now, edited.UpdatedAt)
	assert.Len(t, f.movements.movements, 1, "only the OPENING")

	now = created.Add(2 * time.Hour)
	same, err := f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{
		UserId: user, Id: voo.Id, ExpectedReturnPct: inbound.Change[float64]{Set: true, Value: ptr(8.00)},
	})
	require.NoError(t, err)
	assert.Equal(t, created.Add(time.Hour), same.UpdatedAt)
	untouched, err := f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{UserId: user, Id: voo.Id, Name: ptr("VOO")})
	require.NoError(t, err)
	assert.Equal(t, "8", returnOf(untouched))

	cleared, err := f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{
		UserId: user, Id: voo.Id, ExpectedReturnPct: inbound.Change[float64]{Set: true},
	})
	require.NoError(t, err)
	assert.Nil(t, cleared.ExpectedReturnPct)

	_, err = f.holdingSvc.UpdateHolding(ctx, inbound.UpdateHoldingCommand{
		UserId: user, Id: voo.Id, ExpectedReturnPct: inbound.Change[float64]{Set: true, Value: ptr(101.0)},
	})
	assert.ErrorIs(t, err, model.ErrExpectedReturnOutOfRange)
}

func TestHoldingService_SetsManyExpectedReturnsAtOnce(t *testing.T) {
	created := time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)
	now := created
	f := newLedgerFixture(func() time.Time { return now })
	ctx := context.Background()
	user, other := model.NewUserId(uuid.New()), model.NewUserId(uuid.New())
	add := func(u model.UserId, name string, pct *float64) model.Holding {
		h, err := f.holdingSvc.CreateHolding(ctx, inbound.CreateHoldingCommand{
			UserId: u, Name: name, AssetClass: "Cash", Platform: "Bank", ValueUsd: 100, ExpectedReturnPct: pct,
		})
		require.NoError(t, err)
		return h
	}
	cash, bond, etf := add(user, "Cash", nil), add(user, "Bond", ptr(4.0)), add(user, "ETF", ptr(7.0))
	theirs := add(other, "Theirs", ptr(1.0))

	now = created.Add(time.Hour)
	result, err := f.holdingSvc.SetExpectedReturns(ctx, user, []inbound.ExpectedReturnItem{
		{HoldingId: etf.Id, ExpectedReturnPct: ptr(7.0)}, // as it is: nothing written
		{HoldingId: cash.Id, ExpectedReturnPct: ptr(0.5)},
		{HoldingId: bond.Id}, // cleared
	})
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, []string{"7", "0.5", ""}, []string{returnOf(result[0]), returnOf(result[1]), returnOf(result[2])})
	assert.Equal(t, created, result[0].UpdatedAt, "unchanged keeps its UpdatedAt")
	assert.Equal(t, now, result[1].UpdatedAt)
	assert.Equal(t, "0.5", returnOf(f.holdings.holdings[cash.Id.String()]))
	assert.Nil(t, f.holdings.holdings[bond.Id.String()].ExpectedReturnPct)
	assert.Len(t, f.movements.movements, 4, "returns record no movement")

	// Someone else's holding, or one that doesn't exist: nothing at all is written.
	writes := f.holdings.returnWrites
	for _, id := range []model.HoldingId{theirs.Id, model.NewHoldingId()} {
		_, err = f.holdingSvc.SetExpectedReturns(ctx, user, []inbound.ExpectedReturnItem{
			{HoldingId: cash.Id, ExpectedReturnPct: ptr(9.0)},
			{HoldingId: id, ExpectedReturnPct: ptr(9.0)},
		})
		assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
		assert.EqualError(t, err, "Holding "+id.String()+" not found")
	}
	assert.Equal(t, writes, f.holdings.returnWrites)
	assert.Equal(t, "0.5", returnOf(f.holdings.holdings[cash.Id.String()]))
	assert.Equal(t, "1", returnOf(f.holdings.holdings[theirs.Id.String()]))

	// Out of range: refused before anything is read.
	calls := f.tx.calls
	_, err = f.holdingSvc.SetExpectedReturns(ctx, user, []inbound.ExpectedReturnItem{{HoldingId: cash.Id, ExpectedReturnPct: ptr(-101.0)}})
	assert.ErrorIs(t, err, model.ErrExpectedReturnOutOfRange)
	assert.Equal(t, calls, f.tx.calls)

	// Nothing to change, nothing written.
	result, err = f.holdingSvc.SetExpectedReturns(ctx, user, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.Equal(t, writes, f.holdings.returnWrites)

	// A holding removed between the read and the write.
	f.holdings.beforeReturns = func() { delete(f.holdings.holdings, etf.Id.String()) }
	_, err = f.holdingSvc.SetExpectedReturns(ctx, user, []inbound.ExpectedReturnItem{{HoldingId: etf.Id, ExpectedReturnPct: ptr(1.0)}})
	assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
	f.holdings.beforeReturns = nil

	f.holdings.setReturnsErr = errors.New("write failed")
	_, err = f.holdingSvc.SetExpectedReturns(ctx, user, []inbound.ExpectedReturnItem{{HoldingId: cash.Id, ExpectedReturnPct: ptr(2.0)}})
	assert.EqualError(t, err, "write failed")
}

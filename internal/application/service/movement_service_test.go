package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var movementNow = time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)

// account is a user with holdings, over a ledger fixture.
type account struct {
	*ledgerFixture
	t    *testing.T
	user model.UserId
}

func newAccount(t *testing.T) *account {
	return &account{ledgerFixture: newLedgerFixture(fixedClock(movementNow)), t: t, user: model.NewUserId(uuid.New())}
}

func (a *account) add(name, platform string, value float64) model.Holding {
	a.t.Helper()
	h, err := a.holdingSvc.CreateHolding(context.Background(), inbound.CreateHoldingCommand{
		UserId: a.user, Name: name, AssetClass: "Cash", Platform: platform, ValueUsd: value,
	})
	require.NoError(a.t, err)
	return h
}

func (a *account) value(h model.Holding) string {
	return a.holdings.holdings[h.Id.String()].Value.String()
}

func (a *account) record(cmd inbound.RecordMovementCommand) (inbound.MovementView, error) {
	cmd.UserId = a.user
	return a.movementSvc.RecordMovement(context.Background(), cmd)
}

func TestMovementService_GainsLossesDepositsAndWithdrawalsChangeTheValue(t *testing.T) {
	a := newAccount(t)
	savings := a.add("Savings", "Bank", 100)

	for _, tc := range []struct {
		kind  string
		value string
	}{{"GAIN", "125.50"}, {"LOSS", "100.00"}, {"DEPOSIT", "125.50"}, {"WITHDRAWAL", "100.00"}} {
		view, err := a.record(inbound.RecordMovementCommand{Kind: tc.kind, HoldingId: &savings.Id, AmountUsd: 25.5, Note: "  monthly "})
		require.NoError(t, err, tc.kind)
		assert.Equal(t, tc.value, a.value(savings), tc.kind)
		m := view.Movement
		assert.Equal(t, model.MovementKind(tc.kind), m.Kind)
		assert.Equal(t, "25.50", m.Amount.String())
		assert.Equal(t, savings.Id, m.Holding.Id)
		assert.Equal(t, "Savings", m.Holding.Name)
		assert.Equal(t, movementNow, m.OccurredAt, "now, when not said")
		assert.Equal(t, movementNow, m.CreatedAt)
		assert.Equal(t, "monthly", m.Note)
		assert.Nil(t, m.ToHolding)
		assert.True(t, view.HoldingExists)
		assert.True(t, view.Revertible)
	}
	assert.Equal(t, movementNow, a.holdings.holdings[savings.Id.String()].UpdatedAt)
	assert.Equal(t, 5, a.quotas.movementsOf(a.user), "the OPENING and four movements")

	lastWeek := movementNow.AddDate(0, 0, -7)
	view, err := a.record(inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &savings.Id, AmountUsd: 1, OccurredAt: &lastWeek})
	require.NoError(t, err)
	assert.Equal(t, lastWeek, view.Movement.OccurredAt)
}

func TestMovementService_NeverTakesAHoldingBelowZero(t *testing.T) {
	a := newAccount(t)
	savings := a.add("Savings", "Bank", 1000)

	for kind, why := range map[string]string{
		"LOSS":       "Savings is worth $1,000.00: a loss can't be larger than that.",
		"WITHDRAWAL": "Savings is worth $1,000.00: you can't withdraw more than that.",
	} {
		_, err := a.record(inbound.RecordMovementCommand{Kind: kind, HoldingId: &savings.Id, AmountUsd: 1000.01})
		assert.ErrorAs(t, err, &appErrors.InsufficientBalanceError{})
		assert.EqualError(t, err, why)
	}
	assert.Equal(t, "1000.00", a.value(savings))
	assert.Len(t, a.movements.movements, 1, "just the OPENING")

	// Down to exactly zero is fine.
	_, err := a.record(inbound.RecordMovementCommand{Kind: "WITHDRAWAL", HoldingId: &savings.Id, AmountUsd: 1000})
	require.NoError(t, err)
	assert.Equal(t, "0.00", a.value(savings))
}

func TestMovementService_ValidatesBeforeReading(t *testing.T) {
	a := newAccount(t)
	id := model.NewHoldingId()
	a.holdings.findErr = errors.New("must not be read")
	future := movementNow.Add(25 * time.Hour)
	other := model.NewHoldingId()

	for name, tc := range map[string]struct {
		cmd   inbound.RecordMovementCommand
		check func(t *testing.T, err error)
	}{
		"unknown kind":                 {inbound.RecordMovementCommand{Kind: "REFUND", HoldingId: &id, AmountUsd: 1}, validation("kind")},
		"a kind the system records":    {inbound.RecordMovementCommand{Kind: "OPENING", HoldingId: &id, AmountUsd: 1}, validation("kind")},
		"no amount":                    {inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &id}, is(model.ErrNonPositiveAmount)},
		"negative amount":              {inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &id, AmountUsd: -5}, is(model.ErrNegativeMoney)},
		"long note":                    {inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &id, AmountUsd: 1, Note: strings.Repeat("x", 201)}, is(model.ErrLabelTooLong)},
		"in the future":                {inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &id, AmountUsd: 1, OccurredAt: &future}, is(model.ErrOccurredAtOutOfRange)},
		"no holding":                   {inbound.RecordMovementCommand{Kind: "GAIN", AmountUsd: 1}, validation("holdingId")},
		"transfer without origin":      {inbound.RecordMovementCommand{Kind: "TRANSFER", ToHoldingId: &id, AmountUsd: 1}, validation("fromHoldingId")},
		"transfer without destination": {inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &id, AmountUsd: 1}, validation("toHoldingId")},
		"transfer to both": {inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &id, ToHoldingId: &other,
			ToNewHolding: &inbound.NewHoldingInput{Name: "x", AssetClass: "Cash", Platform: "Bank"}, AmountUsd: 1}, validation("toHoldingId")},
		"transfer to itself":       {inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &id, ToHoldingId: &id, AmountUsd: 1}, validation("toHoldingId")},
		"fee above the amount":     {inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &id, ToHoldingId: &other, AmountUsd: 1, FeeUsd: 1.5}, is(model.ErrFeeExceedsAmount)},
		"negative fee":             {inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &id, ToHoldingId: &other, AmountUsd: 1, FeeUsd: -1}, is(model.ErrNegativeMoney)},
		"new destination unnamed":  {inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &id, ToNewHolding: &inbound.NewHoldingInput{Name: " ", AssetClass: "Cash", Platform: "Bank"}, AmountUsd: 1}, is(model.ErrBlankLabel)},
		"new destination platform": {inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &id, ToNewHolding: &inbound.NewHoldingInput{Name: "x", AssetClass: "Cash", Platform: strings.Repeat("p", 121)}, AmountUsd: 1}, is(model.ErrLabelTooLong)},
		"new destination class":    {inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &id, ToNewHolding: &inbound.NewHoldingInput{Name: "x", AssetClass: "", Platform: "Bank"}, AmountUsd: 1}, is(model.ErrBlankLabel)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := a.record(tc.cmd)
			tc.check(t, err)
			assert.Zero(t, a.tx.calls, "nothing was read")
		})
	}
}

func validation(field string) func(t *testing.T, err error) {
	return func(t *testing.T, err error) {
		var v appErrors.ValidationErrors
		require.ErrorAs(t, err, &v)
		require.NotEmpty(t, v.Errors)
		assert.Equal(t, field, v.Errors[0].Field)
	}
}

func is(target error) func(t *testing.T, err error) {
	return func(t *testing.T, err error) { assert.ErrorIs(t, err, target) }
}

func TestMovementService_OnlyTheUsersOwnHoldings(t *testing.T) {
	a, b := newAccount(t), newAccount(t)
	b.ledgerFixture = a.ledgerFixture // one store, two users
	mine := a.add("Mine", "Bank", 100)
	theirs := b.add("Theirs", "Bank", 100)

	for _, cmd := range []inbound.RecordMovementCommand{
		{Kind: "GAIN", HoldingId: &theirs.Id, AmountUsd: 1},
		{Kind: "TRANSFER", FromHoldingId: &theirs.Id, ToHoldingId: &mine.Id, AmountUsd: 1},
		{Kind: "TRANSFER", FromHoldingId: &mine.Id, ToHoldingId: &theirs.Id, AmountUsd: 1},
	} {
		_, err := a.record(cmd)
		assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{}, cmd.Kind)
	}
	assert.Equal(t, "100.00", a.value(mine))
	assert.Equal(t, "100.00", a.value(theirs))
}

func TestMovementService_TransfersBetweenHoldings(t *testing.T) {
	a := newAccount(t)
	bank := a.add("Savings", "Santander", 9500)
	broker := a.add("USD cash", "Balanz", 0)

	view, err := a.record(inbound.RecordMovementCommand{
		Kind: "TRANSFER", FromHoldingId: &bank.Id, ToHoldingId: &broker.Id, AmountUsd: 1000, FeeUsd: 2.5,
	})
	require.NoError(t, err)
	assert.Equal(t, "8500.00", a.value(bank))
	assert.Equal(t, "997.50", a.value(broker))
	m := view.Movement
	assert.Equal(t, model.MovementTransfer, m.Kind)
	assert.Equal(t, "1000.00", m.Amount.String())
	assert.Equal(t, "2.50", m.Fee.String())
	assert.Equal(t, bank.Id, m.Holding.Id)
	assert.Equal(t, broker.Id, m.ToHolding.Id)
	assert.True(t, view.HoldingExists && view.ToHoldingExists && view.Revertible)

	// Not more than the origin has; and nothing moves when it's refused.
	_, err = a.record(inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &bank.Id, ToHoldingId: &broker.Id, AmountUsd: 8500.01})
	assert.EqualError(t, err, "Savings is worth $8,500.00: you can't transfer more than that.")
	assert.Equal(t, "8500.00", a.value(bank))
	assert.Equal(t, "997.50", a.value(broker))

	missing := model.NewHoldingId()
	_, err = a.record(inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &bank.Id, ToHoldingId: &missing, AmountUsd: 1})
	assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
}

func TestMovementService_TransfersToANewHolding(t *testing.T) {
	a := newAccount(t)
	bank := a.add("Savings", "Santander", 9500)
	a.add("Stocks", "Balanz", 10)

	view, err := a.record(inbound.RecordMovementCommand{
		Kind: "TRANSFER", FromHoldingId: &bank.Id, AmountUsd: 1000, FeeUsd: 1,
		ToNewHolding: &inbound.NewHoldingInput{Name: " USD  cash ", AssetClass: "Cash", Platform: "BALANZ"},
	})
	require.NoError(t, err)
	fresh, ok := a.holdings.holdings[view.Movement.ToHolding.Id.String()]
	require.True(t, ok)
	assert.Equal(t, "USD cash", fresh.Name)
	assert.Equal(t, "Balanz", fresh.Platform.Value(), "spelled as the user's other holdings spell it")
	assert.Equal(t, "999.00", fresh.Value.String())
	assert.Equal(t, movementNow, fresh.CreatedAt)
	assert.Equal(t, "8500.00", a.value(bank))
	assert.Len(t, a.holdings.holdings, 3)
}

func TestMovementService_ANewDestinationRespectsTheHoldingsCap(t *testing.T) {
	a := newAccount(t)
	bank := a.add("Savings", "Santander", 100)
	for len(a.holdings.holdings) < model.MaxHoldingsPerUser {
		h := model.Holding{Id: model.NewHoldingId(), UserId: a.user, Platform: model.MustPlatformName("Bank"), Value: model.ZeroMoney}
		a.holdings.holdings[h.Id.String()] = h
	}
	cmd := inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &bank.Id, AmountUsd: 10,
		ToNewHolding: &inbound.NewHoldingInput{Name: "New", AssetClass: "Cash", Platform: "Elsewhere"}}

	_, err := a.record(cmd)
	assert.ErrorAs(t, err, &appErrors.LimitExceededError{})
	assert.Equal(t, "100.00", a.value(bank))
	assert.Len(t, a.holdings.holdings, model.MaxHoldingsPerUser)

	a.holdings.countErr = errors.New("count failed")
	_, err = a.record(cmd)
	assert.EqualError(t, err, "count failed")
}

func TestMovementService_StorageErrorsLeaveNothingHalfDone(t *testing.T) {
	commands := map[string]func(bank, broker model.HoldingId) inbound.RecordMovementCommand{
		"deposit": func(bank, _ model.HoldingId) inbound.RecordMovementCommand {
			return inbound.RecordMovementCommand{Kind: "DEPOSIT", HoldingId: &bank, AmountUsd: 5}
		},
		"transfer": func(bank, broker model.HoldingId) inbound.RecordMovementCommand {
			return inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &bank, ToHoldingId: &broker, AmountUsd: 5}
		},
		"transfer to a new holding": func(bank, _ model.HoldingId) inbound.RecordMovementCommand {
			return inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &bank, AmountUsd: 5,
				ToNewHolding: &inbound.NewHoldingInput{Name: "N", AssetClass: "Cash", Platform: "Nuevo"}}
		},
	}
	breaks := map[string]func(a *account){
		"the holding can't be read":    func(a *account) { a.holdings.findErr = errors.New("boom") },
		"a holding can't be written":   func(a *account) { a.holdings.updateErr = errors.New("boom") },
		"the movement can't be stored": func(a *account) { a.movements.saveErr = errors.New("boom") },
		"the quota can't be read":      func(a *account) { a.quotas.reserveErr = errors.New("boom") },
		"the movements quota is full":  func(a *account) { a.quotas.counts[a.user.String()+"/movements"] = model.MaxMovementsPerUser },
		"the platform can't be read":   func(a *account) { a.platforms.canonicalErr = errors.New("boom") },
	}
	for breakName, breakIt := range breaks {
		for cmdName, command := range commands {
			if breakName == "the platform can't be read" && cmdName != "transfer to a new holding" {
				continue // only a new destination reads the platforms
			}
			t.Run(breakName+"/"+cmdName, func(t *testing.T) {
				a := newAccount(t)
				bank := a.add("Savings", "Santander", 100)
				broker := a.add("Cash", "Balanz", 0)
				recorded := len(a.movements.movements)
				breakIt(a)

				_, err := a.record(command(bank.Id, broker.Id))

				require.Error(t, err)
				a.holdings.findErr = nil
				assert.Equal(t, "100.00", a.value(bank))
				assert.Equal(t, "0.00", a.value(broker))
				assert.Len(t, a.holdings.holdings, 2)
				assert.Len(t, a.movements.movements, recorded)
			})
		}
	}
}

func TestMovementService_ListsTheLogWithWhatStillExists(t *testing.T) {
	a := newAccount(t)
	bank := a.add("Savings", "Santander", 100)
	broker := a.add("Cash", "Balanz", 0)
	_, err := a.record(inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &bank.Id, ToHoldingId: &broker.Id, AmountUsd: 10})
	require.NoError(t, err)
	_, err = a.record(inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &bank.Id, AmountUsd: 1})
	require.NoError(t, err)
	require.NoError(t, a.holdingSvc.DeleteHolding(context.Background(), a.user, broker.Id))

	list, err := a.movementSvc.ListMovements(context.Background(), a.user, inbound.MovementQuery{Limit: 50})
	require.NoError(t, err)
	require.Len(t, list.Items, 5)
	byKind := map[model.MovementKind]inbound.MovementView{}
	for _, v := range list.Items {
		byKind[v.Movement.Kind] = v
	}
	assert.True(t, byKind[model.MovementGain].Revertible)
	transfer := byKind[model.MovementTransfer]
	assert.True(t, transfer.HoldingExists)
	assert.False(t, transfer.ToHoldingExists, "the destination was removed")
	assert.False(t, transfer.Revertible)
	assert.False(t, byKind[model.MovementClosing].Revertible)
	assert.False(t, byKind[model.MovementClosing].HoldingExists)
	assert.Nil(t, list.Next)

	// One holding's log, a page at a time.
	page, err := a.movementSvc.ListMovements(context.Background(), a.user, inbound.MovementQuery{Filter: model.MovementFilter{HoldingId: &bank.Id}, Limit: 2})
	require.NoError(t, err)
	assert.Len(t, page.Items, 2)
	require.NotNil(t, page.Next)
	rest, err := a.movementSvc.ListMovements(context.Background(), a.user, inbound.MovementQuery{Filter: model.MovementFilter{HoldingId: &bank.Id}, After: page.Next, Limit: 2})
	require.NoError(t, err)
	assert.Len(t, rest.Items, 1)

	a.holdings.existingErr = errors.New("ids failed")
	_, err = a.movementSvc.ListMovements(context.Background(), a.user, inbound.MovementQuery{Limit: 50})
	assert.EqualError(t, err, "ids failed")
	a.movements.listErr = errors.New("list failed")
	_, err = a.movementSvc.ListMovements(context.Background(), a.user, inbound.MovementQuery{Limit: 50})
	assert.EqualError(t, err, "list failed")
}

func TestMovementService_UndoesAMovementAsADelta(t *testing.T) {
	a := newAccount(t)
	bank := a.add("Savings", "Santander", 100)
	broker := a.add("Cash", "Balanz", 0)
	ctx := context.Background()

	gain, err := a.record(inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &bank.Id, AmountUsd: 50})
	require.NoError(t, err)
	transfer, err := a.record(inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &bank.Id, ToHoldingId: &broker.Id, AmountUsd: 30, FeeUsd: 1})
	require.NoError(t, err)
	_, err = a.record(inbound.RecordMovementCommand{Kind: "DEPOSIT", HoldingId: &bank.Id, AmountUsd: 5})
	require.NoError(t, err)
	assert.Equal(t, "125.00", a.value(bank))
	assert.Equal(t, "29.00", a.value(broker))
	counted := a.quotas.movementsOf(a.user)

	// Undoing the transfer puts both back, and keeps the deposit recorded after it.
	require.NoError(t, a.movementSvc.RevertMovement(ctx, a.user, transfer.Movement.Id))
	assert.Equal(t, "155.00", a.value(bank))
	assert.Equal(t, "0.00", a.value(broker))
	assert.Equal(t, counted-1, a.quotas.movementsOf(a.user))

	require.NoError(t, a.movementSvc.RevertMovement(ctx, a.user, gain.Movement.Id))
	assert.Equal(t, "105.00", a.value(bank))

	// Gone already: not found.
	err = a.movementSvc.RevertMovement(ctx, a.user, gain.Movement.Id)
	assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
	assert.ErrorContains(t, err, gain.Movement.Id.String())
}

func TestMovementService_UndoesAnEditedValue(t *testing.T) {
	a := newAccount(t)
	bank := a.add("Savings", "Santander", 100)
	_, err := a.holdingSvc.UpdateHolding(context.Background(), inbound.UpdateHoldingCommand{UserId: a.user, Id: bank.Id, ValueUsd: ptr(80.0), ValueChangeReason: "CORRECTION"})
	require.NoError(t, err)
	edit := a.movements.movements[len(a.movements.movements)-1]
	require.Equal(t, model.MovementAdjustment, edit.Kind)

	require.NoError(t, a.movementSvc.RevertMovement(context.Background(), a.user, edit.Id))
	assert.Equal(t, "100.00", a.value(bank))
}

func TestMovementService_WhatCantBeUndone(t *testing.T) {
	a := newAccount(t)
	bank := a.add("Savings", "Santander", 100)
	broker := a.add("Cash", "Balanz", 0)
	ctx := context.Background()
	opening := a.movements.movements[0]

	err := a.movementSvc.RevertMovement(ctx, a.user, opening.Id)
	assert.ErrorAs(t, err, &appErrors.NotRevertibleError{})
	assert.EqualError(t, err, "Adding or removing an asset can't be undone here: remove it, or add it again.")

	// A holding the movement touched is gone.
	transfer, err := a.record(inbound.RecordMovementCommand{Kind: "TRANSFER", FromHoldingId: &bank.Id, ToHoldingId: &broker.Id, AmountUsd: 10})
	require.NoError(t, err)
	require.NoError(t, a.holdingSvc.DeleteHolding(ctx, a.user, broker.Id))
	assert.EqualError(t, a.movementSvc.RevertMovement(ctx, a.user, transfer.Movement.Id), "Cash was removed, so this can't be undone.")
	assert.Equal(t, "90.00", a.value(bank), "the origin wasn't touched")

	// Undoing a gain that was spent since would go below zero.
	gain, err := a.record(inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &bank.Id, AmountUsd: 50})
	require.NoError(t, err)
	_, err = a.record(inbound.RecordMovementCommand{Kind: "WITHDRAWAL", HoldingId: &bank.Id, AmountUsd: 130})
	require.NoError(t, err)
	err = a.movementSvc.RevertMovement(ctx, a.user, gain.Movement.Id)
	assert.ErrorAs(t, err, &appErrors.InsufficientBalanceError{})
	assert.EqualError(t, err, "Savings is worth $10.00: undoing this would take it below zero.")

	// Someone else's movement is as missing as one that never was.
	other := model.NewUserId(uuid.New())
	assert.ErrorAs(t, a.movementSvc.RevertMovement(ctx, other, gain.Movement.Id), &appErrors.ResourceNotFoundError{})
}

func TestMovementService_UndoReturnsStorageErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		breakIt func(a *account)
		want    string
	}{
		"the movement can't be read":    {func(a *account) { a.movements.findErr = errors.New("find movement") }, "find movement"},
		"the holding can't be read":     {func(a *account) { a.holdings.findErr = errors.New("find holding") }, "find holding"},
		"the holding can't be written":  {func(a *account) { a.holdings.updateErr = errors.New("update holding") }, "update holding"},
		"the movement can't be deleted": {func(a *account) { a.movements.deleteErr = errors.New("delete movement") }, "delete movement"},
		"the quota can't be released":   {func(a *account) { a.quotas.releaseErr = errors.New("release") }, "release"},
	} {
		t.Run(name, func(t *testing.T) {
			a := newAccount(t)
			bank := a.add("Savings", "Santander", 100)
			gain, err := a.record(inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &bank.Id, AmountUsd: 5})
			require.NoError(t, err)
			tc.breakIt(a)

			assert.EqualError(t, a.movementSvc.RevertMovement(context.Background(), a.user, gain.Movement.Id), tc.want)
			assert.Equal(t, "105.00", a.value(bank), "rolled back")
			assert.Len(t, a.movements.movements, 2)
		})
	}
}

func TestMovementService_UndoOfAMovementDeletedMeanwhileIsNotFound(t *testing.T) {
	a := newAccount(t)
	bank := a.add("Savings", "Santander", 100)
	gain, err := a.record(inbound.RecordMovementCommand{Kind: "GAIN", HoldingId: &bank.Id, AmountUsd: 5})
	require.NoError(t, err)
	// Read, then gone before the delete (undone from another device).
	a.movements.findOverride = &gain.Movement
	a.movements.movements = a.movements.movements[:1]

	assert.ErrorAs(t, a.movementSvc.RevertMovement(context.Background(), a.user, gain.Movement.Id), &appErrors.ResourceNotFoundError{})
}

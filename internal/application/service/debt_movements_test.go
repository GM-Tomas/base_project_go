package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovementService_PaysADebtFromAHoldingOrNot(t *testing.T) {
	a := newAccount(t)
	savings := a.add("Savings", "Bank", 1000)
	visa := a.addDebt("Visa", 1250)

	view, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &visa.Id, FromHoldingId: &savings.Id, AmountUsd: 300})
	require.NoError(t, err)
	assert.Equal(t, "950.00", a.balance(visa))
	assert.Equal(t, "700.00", a.value(savings))
	m := view.Movement
	assert.Equal(t, model.MovementDebtPayment, m.Kind)
	assert.Equal(t, &model.DebtRef{Id: visa.Id, Name: "Visa"}, m.Debt)
	assert.Equal(t, savings.Id, m.Holding.Id, "where the money came from")
	assert.Nil(t, m.ToHolding)
	assert.True(t, view.DebtExists && view.HoldingExists && view.Revertible)
	assert.Equal(t, movementNow, a.debts.debts[visa.Id.String()].UpdatedAt)
	assert.Equal(t, movementNow, a.holdings.holdings[savings.Id.String()].UpdatedAt)

	// Paid from somewhere BASE doesn't track: only the debt changes.
	view, err = a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &visa.Id, AmountUsd: 950})
	require.NoError(t, err)
	assert.Equal(t, "0.00", a.balance(visa))
	assert.Nil(t, view.Movement.Holding)
	assert.False(t, view.HoldingExists)
}

func TestMovementService_ChargesAndInterestRaiseADebt(t *testing.T) {
	a := newAccount(t)
	savings := a.add("Savings", "Bank", 100)
	loan := a.addDebt("Loan", 0)

	// A loan paid into an account: both go up.
	view, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_CHARGE", DebtId: &loan.Id, ToHoldingId: &savings.Id, AmountUsd: 10000})
	require.NoError(t, err)
	assert.Equal(t, "10000.00", a.balance(loan))
	assert.Equal(t, "10100.00", a.value(savings))
	assert.Equal(t, savings.Id, view.Movement.ToHolding.Id, "where the money went")
	assert.Nil(t, view.Movement.Holding)
	assert.True(t, view.ToHoldingExists)

	_, err = a.record(inbound.RecordMovementCommand{Kind: "DEBT_CHARGE", DebtId: &loan.Id, AmountUsd: 50})
	require.NoError(t, err)
	_, err = a.record(inbound.RecordMovementCommand{Kind: "DEBT_INTEREST", DebtId: &loan.Id, AmountUsd: 42.1, FromHoldingId: &savings.Id})
	require.NoError(t, err)
	assert.Equal(t, "10092.10", a.balance(loan))
	assert.Equal(t, "10100.00", a.value(savings), "interest takes nothing from a holding")
}

func TestMovementService_NeverPaysMoreThanIsOwedOrThere(t *testing.T) {
	a := newAccount(t)
	savings := a.add("Savings", "Bank", 100)
	visa := a.addDebt("Visa", 1250)

	_, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &visa.Id, AmountUsd: 1250.01})
	assert.ErrorAs(t, err, &appErrors.InsufficientBalanceError{})
	assert.EqualError(t, err, "Visa only has $1,250.00 left to pay.")

	_, err = a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &visa.Id, FromHoldingId: &savings.Id, AmountUsd: 100.01})
	assert.ErrorAs(t, err, &appErrors.InsufficientBalanceError{})
	assert.EqualError(t, err, "Savings is worth $100.00: you can't pay more than that.")

	assert.Equal(t, "1250.00", a.balance(visa))
	assert.Equal(t, "100.00", a.value(savings))
	assert.Len(t, a.movements.movements, 2, "the two OPENINGs")
}

func TestMovementService_DebtMovementsNeedTheUsersOwnDebt(t *testing.T) {
	a, b := newAccount(t), newAccount(t)
	b.ledgerFixture = a.ledgerFixture
	mine := a.add("Mine", "Bank", 100)
	myDebt := a.addDebt("Visa", 100)
	theirDebt := b.addDebt("Theirs", 100)
	theirHolding := b.add("Theirs", "Bank", 100)

	_, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_INTEREST", AmountUsd: 1})
	var v appErrors.ValidationErrors
	require.ErrorAs(t, err, &v)
	assert.Equal(t, "debtId", v.Errors[0].Field)
	assert.EqualError(t, err, "validation failed with 1 errors")

	for _, cmd := range []inbound.RecordMovementCommand{
		{Kind: "DEBT_PAYMENT", DebtId: &theirDebt.Id, FromHoldingId: &mine.Id, AmountUsd: 1},
		{Kind: "DEBT_PAYMENT", DebtId: &myDebt.Id, FromHoldingId: &theirHolding.Id, AmountUsd: 1},
		{Kind: "DEBT_CHARGE", DebtId: &myDebt.Id, ToHoldingId: &theirHolding.Id, AmountUsd: 1},
	} {
		_, err := a.record(cmd)
		assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{}, cmd.Kind)
	}
	assert.Equal(t, "100.00", a.balance(myDebt))
	assert.Equal(t, "100.00", a.value(mine))
	assert.Equal(t, "100.00", a.balance(theirDebt))
	assert.Equal(t, "100.00", a.value(theirHolding))
}

func TestMovementService_DebtMovementsAreAllOrNothing(t *testing.T) {
	for name, breakIt := range map[string]func(a *account){
		"the debt can't be read":       func(a *account) { a.debts.findErr = errors.New("boom") },
		"the debt can't be written":    func(a *account) { a.debts.updateErr = errors.New("boom") },
		"the holding can't be written": func(a *account) { a.holdings.updateErr = errors.New("boom") },
		"the movement can't be stored": func(a *account) { a.movements.saveErr = errors.New("boom") },
		"the debt is gone meanwhile":   func(a *account) { a.debts.beforeUpdate = func() { clear(a.debts.debts) } },
	} {
		t.Run(name, func(t *testing.T) {
			a := newAccount(t)
			savings := a.add("Savings", "Bank", 100)
			visa := a.addDebt("Visa", 100)
			breakIt(a)
			_, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &visa.Id, FromHoldingId: &savings.Id, AmountUsd: 10})
			require.Error(t, err)
			a.debts.findErr, a.debts.beforeUpdate = nil, nil
			assert.Equal(t, "100.00", a.balance(visa))
			assert.Equal(t, "100.00", a.value(savings))
			assert.Len(t, a.movements.movements, 2)
		})
	}
}

func TestMovementService_ListsADebtsLogWithWhatStillExists(t *testing.T) {
	a := newAccount(t)
	savings := a.add("Savings", "Bank", 1000)
	visa := a.addDebt("Visa", 500)
	amex := a.addDebt("Amex", 500)
	_, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &visa.Id, FromHoldingId: &savings.Id, AmountUsd: 100})
	require.NoError(t, err)
	_, err = a.record(inbound.RecordMovementCommand{Kind: "DEBT_INTEREST", DebtId: &amex.Id, AmountUsd: 5})
	require.NoError(t, err)

	list, err := a.movementSvc.ListMovements(context.Background(), a.user, inbound.MovementQuery{Filter: model.MovementFilter{DebtId: &visa.Id}, Limit: 50})
	require.NoError(t, err)
	require.Len(t, list.Items, 2, "Visa's OPENING and payment")
	for _, v := range list.Items {
		assert.Equal(t, visa.Id, v.Movement.Debt.Id)
		assert.True(t, v.DebtExists)
	}

	require.NoError(t, a.debtSvc.DeleteDebt(context.Background(), a.user, visa.Id))
	list, err = a.movementSvc.ListMovements(context.Background(), a.user, inbound.MovementQuery{Filter: model.MovementFilter{DebtId: &visa.Id}, Limit: 50})
	require.NoError(t, err)
	require.Len(t, list.Items, 3)
	for _, v := range list.Items {
		assert.False(t, v.DebtExists, v.Movement.Kind)
		assert.False(t, v.Revertible, "%s: its debt is gone", v.Movement.Kind)
	}

	a.debts.existingErr = errors.New("ids failed")
	_, err = a.movementSvc.ListMovements(context.Background(), a.user, inbound.MovementQuery{Limit: 50})
	assert.EqualError(t, err, "ids failed")
}

func TestMovementService_UndoesDebtMovementsAsDeltas(t *testing.T) {
	a := newAccount(t)
	savings := a.add("Savings", "Bank", 1000)
	visa := a.addDebt("Visa", 500)

	payment, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &visa.Id, FromHoldingId: &savings.Id, AmountUsd: 200})
	require.NoError(t, err)
	_, err = a.record(inbound.RecordMovementCommand{Kind: "DEBT_INTEREST", DebtId: &visa.Id, AmountUsd: 10})
	require.NoError(t, err)
	require.NoError(t, a.movementSvc.RevertMovement(context.Background(), a.user, payment.Movement.Id))
	assert.Equal(t, "510.00", a.balance(visa), "the interest since is kept")
	assert.Equal(t, "1000.00", a.value(savings))

	charge, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_CHARGE", DebtId: &visa.Id, ToHoldingId: &savings.Id, AmountUsd: 300})
	require.NoError(t, err)
	require.NoError(t, a.movementSvc.RevertMovement(context.Background(), a.user, charge.Movement.Id))
	assert.Equal(t, "510.00", a.balance(visa))
	assert.Equal(t, "1000.00", a.value(savings))

	// An edit of the balance too.
	balance := 400.0
	_, err = a.updateDebt(inbound.UpdateDebtCommand{Id: visa.Id, BalanceUsd: &balance})
	require.NoError(t, err)
	edit := a.movements.movements[len(a.movements.movements)-1]
	require.NoError(t, a.movementSvc.RevertMovement(context.Background(), a.user, edit.Id))
	assert.Equal(t, "510.00", a.balance(visa))
	assert.Equal(t, 3, a.quotas.movementsOf(a.user), "the two OPENINGs and the interest")
}

func TestMovementService_WhatCantBeUndoneOnADebt(t *testing.T) {
	a := newAccount(t)
	savings := a.add("Savings", "Bank", 1000)
	visa := a.addDebt("Visa", 500)

	// The charge went into savings, and was spent since.
	charge, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_CHARGE", DebtId: &visa.Id, ToHoldingId: &savings.Id, AmountUsd: 300})
	require.NoError(t, err)
	_, err = a.record(inbound.RecordMovementCommand{Kind: "WITHDRAWAL", HoldingId: &savings.Id, AmountUsd: 1200})
	require.NoError(t, err)
	err = a.movementSvc.RevertMovement(context.Background(), a.user, charge.Movement.Id)
	assert.EqualError(t, err, "Savings is worth $100.00: undoing this would take it below zero.")

	// The interest was paid off since.
	interest, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_INTEREST", DebtId: &visa.Id, AmountUsd: 50})
	require.NoError(t, err)
	_, err = a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &visa.Id, AmountUsd: 830})
	require.NoError(t, err)
	err = a.movementSvc.RevertMovement(context.Background(), a.user, interest.Movement.Id)
	assert.ErrorAs(t, err, &appErrors.InsufficientBalanceError{})
	assert.EqualError(t, err, "Visa has $20.00 left to pay: undoing this would take it below zero.")
	assert.Equal(t, "20.00", a.balance(visa))

	// The debt is gone.
	require.NoError(t, a.debtSvc.DeleteDebt(context.Background(), a.user, visa.Id))
	err = a.movementSvc.RevertMovement(context.Background(), a.user, interest.Movement.Id)
	assert.ErrorAs(t, err, &appErrors.NotRevertibleError{})
	assert.EqualError(t, err, "Visa was removed, so this can't be undone.")
	closing := a.movements.movements[len(a.movements.movements)-1]
	require.Equal(t, model.MovementClosing, closing.Kind)
	err = a.movementSvc.RevertMovement(context.Background(), a.user, closing.Id)
	assert.ErrorAs(t, err, &appErrors.NotRevertibleError{})

	// Reading or writing the debt fails: nothing changes.
	loan := a.addDebt("Loan", 100)
	payment, err := a.record(inbound.RecordMovementCommand{Kind: "DEBT_PAYMENT", DebtId: &loan.Id, AmountUsd: 10})
	require.NoError(t, err)
	a.debts.findErr = errors.New("read failed")
	assert.EqualError(t, a.movementSvc.RevertMovement(context.Background(), a.user, payment.Movement.Id), "read failed")
	a.debts.findErr, a.debts.updateErr = nil, errors.New("write failed")
	assert.EqualError(t, a.movementSvc.RevertMovement(context.Background(), a.user, payment.Movement.Id), "write failed")
	assert.Equal(t, "90.00", a.balance(loan))
}

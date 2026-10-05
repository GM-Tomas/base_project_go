package service_test

import (
	"context"
	"errors"
	"math"
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

// newAccountOn is an account whose services read the time from clock.
func newAccountOn(t *testing.T, clock service.Clock) *account {
	return &account{ledgerFixture: newLedgerFixture(clock), t: t, user: model.NewUserId(uuid.New())}
}

// addDebt adds a debt for the account's user.
func (a *account) addDebt(name string, balance float64, terms ...func(*inbound.CreateDebtCommand)) model.Debt {
	a.t.Helper()
	cmd := inbound.CreateDebtCommand{UserId: a.user, Name: name, BalanceUsd: balance}
	for _, term := range terms {
		term(&cmd)
	}
	view, err := a.debtSvc.CreateDebt(context.Background(), cmd)
	require.NoError(a.t, err)
	return view.Debt
}

func (a *account) balance(d model.Debt) string {
	return a.debts.debts[d.Id.String()].Balance.String()
}

func (a *account) updateDebt(cmd inbound.UpdateDebtCommand) (inbound.DebtView, error) {
	cmd.UserId = a.user
	return a.debtSvc.UpdateDebt(context.Background(), cmd)
}

func paying(payment float64) func(*inbound.CreateDebtCommand) {
	return func(c *inbound.CreateDebtCommand) { c.MonthlyPaymentUsd = &payment }
}

func set[T any](v T) inbound.Change[T] { return inbound.Change[T]{Set: true, Value: &v} }

func TestDebtService_CreatesADebtWithItsOpening(t *testing.T) {
	a := newAccount(t)
	rate, payment, day := 65.0, 300.0, 10
	view, err := a.debtSvc.CreateDebt(context.Background(), inbound.CreateDebtCommand{
		UserId: a.user, Name: "  Visa ", Lender: " Banco  Galicia", Kind: "CREDIT_CARD", BalanceUsd: 1250.404,
		InterestRatePct: &rate, MonthlyPaymentUsd: &payment, DueDay: &day, Notes: " 0% until March ",
	})
	require.NoError(t, err)
	d := view.Debt
	assert.Equal(t, "Visa", d.Name)
	assert.Equal(t, "Banco Galicia", d.Lender)
	assert.Equal(t, model.DebtCreditCard, d.Kind)
	assert.Equal(t, "1250.40", d.Balance.String())
	assert.Equal(t, "65.00", d.InterestRatePct.StringFixed(2))
	assert.Equal(t, "300.00", d.MonthlyPayment.String())
	assert.Equal(t, 10, *d.DueDay)
	assert.Equal(t, "0% until March", d.Notes)
	assert.Equal(t, movementNow, d.CreatedAt)
	assert.Equal(t, movementNow, d.UpdatedAt)
	assert.Equal(t, model.PayoffOnTrack, view.Payoff.Status, "the payoff comes with it")

	require.Len(t, a.movements.movements, 1)
	opening := a.movements.movements[0]
	assert.Equal(t, model.MovementOpening, opening.Kind)
	assert.Equal(t, "1250.40", opening.Amount.String())
	assert.Equal(t, &model.DebtRef{Id: d.Id, Name: "Visa", Lender: "Banco Galicia"}, opening.Debt)
	assert.Nil(t, opening.Holding)
	assert.Equal(t, 1, a.quotas.movementsOf(a.user))

	// Only what's required: no terms, OTHER, a balance of 0.
	bare, err := a.debtSvc.CreateDebt(context.Background(), inbound.CreateDebtCommand{UserId: a.user, Name: "Mom"})
	require.NoError(t, err)
	assert.Equal(t, model.DebtOther, bare.Debt.Kind)
	assert.Nil(t, bare.Debt.InterestRatePct)
	assert.Nil(t, bare.Debt.MonthlyPayment)
	assert.Nil(t, bare.Debt.DueDay)
	assert.Equal(t, model.PayoffPaidOff, bare.Payoff.Status)
}

func TestDebtService_ListsTheUsersDebtsLargestFirst(t *testing.T) {
	a, b := newAccount(t), newAccount(t)
	b.ledgerFixture = a.ledgerFixture // one store, two users
	a.addDebt("Visa", 1250, paying(300))
	a.addDebt("Mortgage", 80000)
	a.addDebt("Amex", 1250)
	b.addDebt("Theirs", 1e6)

	views, err := a.debtSvc.GetDebts(context.Background(), a.user)
	require.NoError(t, err)
	var names []string
	for _, v := range views {
		names = append(names, v.Debt.Name)
	}
	assert.Equal(t, []string{"Mortgage", "Amex", "Visa"}, names)
	assert.Equal(t, model.PayoffNoPayment, views[0].Payoff.Status)
	assert.Equal(t, model.PayoffOnTrack, views[2].Payoff.Status)
	assert.Equal(t, 5, *views[2].Payoff.Months)

	a.debts.findAllErr = errors.New("boom")
	_, err = a.debtSvc.GetDebts(context.Background(), a.user)
	assert.EqualError(t, err, "boom")
}

func TestDebtService_CreateValidatesFirstAndCapsDebts(t *testing.T) {
	a := newAccount(t)
	a.debts.countErr = errors.New("must not be read")
	bad := func(v float64) *float64 { return &v }
	day := func(v int) *int { return &v }
	for name, tc := range map[string]struct {
		cmd    inbound.CreateDebtCommand
		target error
	}{
		"no name":        {inbound.CreateDebtCommand{Name: " "}, model.ErrBlankLabel},
		"long name":      {inbound.CreateDebtCommand{Name: strings.Repeat("x", 121)}, model.ErrLabelTooLong},
		"long lender":    {inbound.CreateDebtCommand{Name: "x", Lender: strings.Repeat("x", 121)}, model.ErrLabelTooLong},
		"unknown kind":   {inbound.CreateDebtCommand{Name: "x", Kind: "CARD"}, model.ErrUnknownDebtKind},
		"negative":       {inbound.CreateDebtCommand{Name: "x", BalanceUsd: -1}, model.ErrNegativeMoney},
		"rate too high":  {inbound.CreateDebtCommand{Name: "x", InterestRatePct: bad(201)}, model.ErrInterestRateOutOfRange},
		"payment below":  {inbound.CreateDebtCommand{Name: "x", MonthlyPaymentUsd: bad(-1)}, model.ErrNegativeMoney},
		"no such day":    {inbound.CreateDebtCommand{Name: "x", DueDay: day(32)}, model.ErrDueDayOutOfRange},
		"long notes":     {inbound.CreateDebtCommand{Name: "x", Notes: strings.Repeat("x", 501)}, model.ErrLabelTooLong},
		"infinite value": {inbound.CreateDebtCommand{Name: "x", BalanceUsd: math.Inf(1)}, model.ErrNonFiniteMoney},
	} {
		t.Run(name, func(t *testing.T) {
			tc.cmd.UserId = a.user
			_, err := a.debtSvc.CreateDebt(context.Background(), tc.cmd)
			assert.ErrorIs(t, err, tc.target)
			assert.Zero(t, a.tx.calls, "nothing was read")
		})
	}

	a.debts.countErr = nil
	for i := 0; i < model.MaxDebtsPerUser; i++ {
		a.debts.debts[model.NewDebtId().String()] = model.Debt{UserId: a.user}
	}
	_, err := a.debtSvc.CreateDebt(context.Background(), inbound.CreateDebtCommand{UserId: a.user, Name: "one more"})
	assert.ErrorAs(t, err, &appErrors.LimitExceededError{})
	assert.EqualError(t, err, "You can track up to 200 debts. Remove one to add another.")
	assert.Len(t, a.debts.debts, model.MaxDebtsPerUser)
	assert.Empty(t, a.movements.movements)
}

func TestDebtService_CreateIsAllOrNothing(t *testing.T) {
	for name, breakIt := range map[string]func(a *account){
		"the debts can't be counted":  func(a *account) { a.debts.countErr = errors.New("boom") },
		"the debt can't be stored":    func(a *account) { a.debts.insertErr = errors.New("boom") },
		"the opening can't be stored": func(a *account) { a.movements.saveErr = errors.New("boom") },
		"the movements quota is full": func(a *account) { a.quotas.counts[a.user.String()+"/movements"] = model.MaxMovementsPerUser },
		"the quota can't be reserved": func(a *account) { a.quotas.reserveErr = errors.New("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			a := newAccount(t)
			breakIt(a)
			_, err := a.debtSvc.CreateDebt(context.Background(), inbound.CreateDebtCommand{UserId: a.user, Name: "Visa", BalanceUsd: 10})
			require.Error(t, err)
			assert.Empty(t, a.debts.debts)
			assert.Empty(t, a.movements.movements)
		})
	}
}

func TestDebtService_UpdateChangesOnlyWhatIsSent(t *testing.T) {
	now := movementNow
	a := newAccountOn(t, func() time.Time { return now })
	rate := 50.0
	visa := a.addDebt("Visa", 1000, paying(300), func(c *inbound.CreateDebtCommand) {
		c.Lender, c.Notes, c.InterestRatePct = "Galicia", "old", &rate
	})
	now = now.Add(time.Hour)
	later := now

	name, kind, lender, notes := "Visa Gold", "CREDIT_CARD", "", " new "
	view, err := a.updateDebt(inbound.UpdateDebtCommand{
		Id: visa.Id, Name: &name, Kind: &kind, Lender: &lender, Notes: &notes,
		InterestRatePct: inbound.Change[float64]{Set: true}, MonthlyPaymentUsd: set(450.0), DueDay: set(5),
	})
	require.NoError(t, err)
	d := view.Debt
	assert.Equal(t, "Visa Gold", d.Name)
	assert.Equal(t, model.DebtCreditCard, d.Kind)
	assert.Empty(t, d.Lender, "cleared")
	assert.Equal(t, "new", d.Notes)
	assert.Nil(t, d.InterestRatePct, "cleared")
	assert.Equal(t, "450.00", d.MonthlyPayment.String())
	assert.Equal(t, 5, *d.DueDay)
	assert.Equal(t, "1000.00", d.Balance.String(), "not sent: as it was")
	assert.Equal(t, later, d.UpdatedAt)
	assert.Equal(t, movementNow, d.CreatedAt)
	assert.Len(t, a.movements.movements, 1, "no new balance, no movement")

	// Nothing new: nothing written, UpdatedAt kept.
	a.debts.updateErr = errors.New("must not write")
	again, err := a.updateDebt(inbound.UpdateDebtCommand{Id: visa.Id, Name: &name, MonthlyPaymentUsd: set(450.0), DueDay: set(5)})
	require.NoError(t, err)
	assert.Equal(t, d, again.Debt)
	_, err = a.updateDebt(inbound.UpdateDebtCommand{Id: visa.Id})
	require.NoError(t, err)
}

func TestDebtService_UpdateRecordsWhyTheBalanceChanged(t *testing.T) {
	a := newAccount(t)
	visa := a.addDebt("Visa", 1000)
	lastWeek := movementNow.AddDate(0, 0, -7)
	steps := []struct {
		balance float64
		reason  string
		kind    model.MovementKind
		amount  string
	}{
		{700, "PAYMENT", model.MovementDebtPayment, "300.00"},
		{1200, "CHARGE", model.MovementDebtCharge, "500.00"},
		{1242.1, "INTEREST", model.MovementDebtInterest, "42.10"},
		{1200, "", model.MovementAdjustment, "42.10"},
		{1300, "CORRECTION", model.MovementAdjustment, "100.00"},
	}
	for i, step := range steps {
		balance := step.balance
		cmd := inbound.UpdateDebtCommand{Id: visa.Id, BalanceUsd: &balance, BalanceChangeReason: step.reason, Note: " statement "}
		if i == 0 {
			cmd.OccurredAt = &lastWeek
		}
		_, err := a.updateDebt(cmd)
		require.NoError(t, err, step.reason)
		m := a.movements.movements[len(a.movements.movements)-1]
		assert.Equal(t, step.kind, m.Kind, step.reason)
		assert.Equal(t, step.amount, m.Amount.String(), step.reason)
		assert.Equal(t, visa.Id, m.Debt.Id)
		assert.Nil(t, m.Holding, "an edit moves no holding's money")
		assert.Equal(t, "statement", m.Note)
		assert.Equal(t, model.MustMoneyFromFloat(balance).String(), m.NewValue.String())
	}
	assert.Equal(t, lastWeek, a.movements.movements[1].OccurredAt)
	assert.Equal(t, "1300.00", a.balance(visa))

	// The reason has to agree with the direction, and nothing changes when it doesn't.
	lower, higher := 1000.0, 2000.0
	_, err := a.updateDebt(inbound.UpdateDebtCommand{Id: visa.Id, BalanceUsd: &higher, BalanceChangeReason: "PAYMENT"})
	assert.ErrorIs(t, err, model.ErrPaymentRaisesBalance)
	_, err = a.updateDebt(inbound.UpdateDebtCommand{Id: visa.Id, BalanceUsd: &lower, BalanceChangeReason: "INTEREST"})
	assert.ErrorIs(t, err, model.ErrChargeLowersBalance)
	assert.Equal(t, "1300.00", a.balance(visa))
	assert.Len(t, a.movements.movements, 6)
}

func TestDebtService_UpdateValidatesBeforeReading(t *testing.T) {
	a := newAccount(t)
	id := model.NewDebtId()
	a.debts.findErr = errors.New("must not be read")
	blank, long, kind, negative := " ", strings.Repeat("x", 501), "CARD", -1.0
	future := movementNow.Add(25 * time.Hour)
	for name, tc := range map[string]struct {
		cmd    inbound.UpdateDebtCommand
		target error
	}{
		"blank name":     {inbound.UpdateDebtCommand{Name: &blank}, model.ErrBlankLabel},
		"long lender":    {inbound.UpdateDebtCommand{Lender: &long}, model.ErrLabelTooLong},
		"unknown kind":   {inbound.UpdateDebtCommand{Kind: &kind}, model.ErrUnknownDebtKind},
		"negative":       {inbound.UpdateDebtCommand{BalanceUsd: &negative}, model.ErrNegativeMoney},
		"rate":           {inbound.UpdateDebtCommand{InterestRatePct: set(-1.0)}, model.ErrInterestRateOutOfRange},
		"payment":        {inbound.UpdateDebtCommand{MonthlyPaymentUsd: set(-1.0)}, model.ErrNegativeMoney},
		"due day":        {inbound.UpdateDebtCommand{DueDay: set(0)}, model.ErrDueDayOutOfRange},
		"long notes":     {inbound.UpdateDebtCommand{Notes: &long}, model.ErrLabelTooLong},
		"unknown reason": {inbound.UpdateDebtCommand{BalanceChangeReason: "GIFT"}, model.ErrUnknownBalanceChangeReason},
		"long note":      {inbound.UpdateDebtCommand{Note: strings.Repeat("x", 201)}, model.ErrLabelTooLong},
		"in the future":  {inbound.UpdateDebtCommand{OccurredAt: &future}, model.ErrOccurredAtOutOfRange},
	} {
		t.Run(name, func(t *testing.T) {
			tc.cmd.Id = id
			_, err := a.updateDebt(tc.cmd)
			assert.ErrorIs(t, err, tc.target)
			assert.Zero(t, a.tx.calls, "nothing was read")
		})
	}
}

func TestDebtService_OnlyTheUsersOwnDebts(t *testing.T) {
	a, b := newAccount(t), newAccount(t)
	b.ledgerFixture = a.ledgerFixture
	theirs := b.addDebt("Theirs", 100)
	name, balance := "Mine now", 0.0

	_, err := a.updateDebt(inbound.UpdateDebtCommand{Id: theirs.Id, Name: &name, BalanceUsd: &balance})
	assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
	assert.EqualError(t, err, "Debt "+theirs.Id.String()+" not found")
	err = a.debtSvc.DeleteDebt(context.Background(), a.user, theirs.Id)
	assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
	assert.Equal(t, "100.00", a.balance(theirs))
	assert.Len(t, a.movements.movements, 1, "just their OPENING")
}

func TestDebtService_DeleteRecordsTheClosing(t *testing.T) {
	a := newAccount(t)
	visa := a.addDebt("Visa", 1250, paying(300))

	require.NoError(t, a.debtSvc.DeleteDebt(context.Background(), a.user, visa.Id))
	assert.Empty(t, a.debts.debts)
	closing := a.movements.movements[1]
	assert.Equal(t, model.MovementClosing, closing.Kind)
	assert.Equal(t, "1250.00", closing.Amount.String())
	assert.Equal(t, visa.Id, closing.Debt.Id)
	assert.Equal(t, 1, a.quotas.movementsOf(a.user), "a CLOSING isn't counted")

	err := a.debtSvc.DeleteDebt(context.Background(), a.user, visa.Id)
	assert.ErrorAs(t, err, &appErrors.ResourceNotFoundError{})
}

func TestDebtService_StorageErrorsLeaveNothingHalfDone(t *testing.T) {
	for name, tc := range map[string]struct {
		breakIt func(a *account)
		act     func(a *account, d model.Debt) error
	}{
		"edit: the debt can't be read":       {func(a *account) { a.debts.findErr = errors.New("boom") }, editBalance},
		"edit: the debt can't be written":    {func(a *account) { a.debts.updateErr = errors.New("boom") }, editBalance},
		"edit: the movement can't be stored": {func(a *account) { a.movements.saveErr = errors.New("boom") }, editBalance},
		"edit: the debt is gone meanwhile": {func(a *account) {
			a.debts.updateErr = nil
			a.debts.beforeUpdate = func() { clear(a.debts.debts) }
		}, editBalance},
		"delete: the debt can't be read":      {func(a *account) { a.debts.findErr = errors.New("boom") }, deleteDebt},
		"delete: the debt can't be deleted":   {func(a *account) { a.debts.deleteErr = errors.New("boom") }, deleteDebt},
		"delete: the closing can't be stored": {func(a *account) { a.movements.saveErr = errors.New("boom") }, deleteDebt},
		"delete: the debt is gone meanwhile": {func(a *account) {
			a.debts.beforeDelete = func() { clear(a.debts.debts) }
		}, deleteDebt},
	} {
		t.Run(name, func(t *testing.T) {
			a := newAccount(t)
			visa := a.addDebt("Visa", 1000)
			tc.breakIt(a)
			require.Error(t, tc.act(a, visa))
			a.debts.findErr, a.debts.beforeUpdate, a.debts.beforeDelete = nil, nil, nil
			assert.Equal(t, "1000.00", a.balance(visa))
			assert.Len(t, a.movements.movements, 1, "just the OPENING")
		})
	}
}

func editBalance(a *account, d model.Debt) error {
	balance := 500.0
	_, err := a.updateDebt(inbound.UpdateDebtCommand{Id: d.Id, BalanceUsd: &balance, BalanceChangeReason: "PAYMENT"})
	return err
}

func deleteDebt(a *account, d model.Debt) error {
	return a.debtSvc.DeleteDebt(context.Background(), a.user, d.Id)
}

package service

import (
	"context"
	"fmt"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	domainService "github.com/GM-Tomas/base_project_go/internal/domain/service"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/shopspring/decimal"
)

var errDebtsLimit = appErrors.NewLimitExceededError(fmt.Sprintf(
	"You can track up to %d debts. Remove one to add another.", model.MaxDebtsPerUser))

// DebtService adds, edits and removes debts. Like a holding's value, each change of a balance is recorded in
// the activity log in the same transaction: an OPENING when a debt is added, a CLOSING when it's removed,
// and what an edit of its balance was (see model.BalanceChangeReason).
type DebtService struct {
	tx     outbound.TransactionManager
	debts  outbound.DebtRepository
	ledger ledger
	clock  Clock
}

func NewDebtService(
	tx outbound.TransactionManager,
	debts outbound.DebtRepository,
	movements outbound.MovementRepository,
	quotas outbound.QuotaRepository,
	clock Clock,
) *DebtService {
	if clock == nil {
		clock = RealClock
	}
	return &DebtService{tx: tx, debts: debts, ledger: ledger{movements: movements, quotas: quotas}, clock: clock}
}

var _ inbound.DebtUseCase = (*DebtService)(nil)

func (s *DebtService) view(d model.Debt) inbound.DebtView {
	return inbound.DebtView{Debt: d, Payoff: domainService.CalculateDebtPayoff(d, s.clock())}
}

func (s *DebtService) GetDebts(ctx context.Context, userId model.UserId) ([]inbound.DebtView, error) {
	debts, err := s.debts.FindAll(ctx, userId)
	if err != nil {
		return nil, err
	}
	views := make([]inbound.DebtView, len(debts))
	for i, d := range debts {
		views[i] = s.view(d)
	}
	return views, nil
}

// interestRate, monthlyPayment and dueDay are a debt's optional terms, validated: nil when not given.
func interestRate(v *float64) (*decimal.Decimal, error) {
	if v == nil {
		return nil, nil
	}
	rate, err := model.NewInterestRatePct(*v)
	return &rate, err
}

func monthlyPayment(v *float64) (*model.Money, error) {
	if v == nil {
		return nil, nil
	}
	m, err := model.NewMoneyFromFloat(*v)
	return &m, err
}

func dueDay(v *int) (*int, error) {
	if v == nil {
		return nil, nil
	}
	day, err := model.NewDueDay(*v)
	return &day, err
}

// CreateDebt validates everything before touching storage, then adds the debt and its OPENING in one
// transaction, where the debts cap is checked too (the OPENING's count serializes concurrent creates, so
// the cap is exact).
func (s *DebtService) CreateDebt(ctx context.Context, cmd inbound.CreateDebtCommand) (inbound.DebtView, error) {
	now := s.clock()
	name, err := model.NormalizeDebtName(cmd.Name)
	if err != nil {
		return inbound.DebtView{}, err
	}
	lender, err := model.NormalizeDebtLender(cmd.Lender)
	if err != nil {
		return inbound.DebtView{}, err
	}
	kind, err := model.ParseDebtKind(cmd.Kind)
	if err != nil {
		return inbound.DebtView{}, err
	}
	balance, err := model.NewMoneyFromFloat(cmd.BalanceUsd)
	if err != nil {
		return inbound.DebtView{}, err
	}
	rate, err := interestRate(cmd.InterestRatePct)
	if err != nil {
		return inbound.DebtView{}, err
	}
	payment, err := monthlyPayment(cmd.MonthlyPaymentUsd)
	if err != nil {
		return inbound.DebtView{}, err
	}
	day, err := dueDay(cmd.DueDay)
	if err != nil {
		return inbound.DebtView{}, err
	}
	notes, err := model.NormalizeDebtNotes(cmd.Notes)
	if err != nil {
		return inbound.DebtView{}, err
	}
	debt := model.Debt{
		Id: model.NewDebtId(), UserId: cmd.UserId, Name: name, Lender: lender, Kind: kind, Balance: balance,
		InterestRatePct: rate, MonthlyPayment: payment, DueDay: day, Notes: notes, CreatedAt: now, UpdatedAt: now,
	}

	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		count, err := s.debts.Count(ctx, cmd.UserId)
		if err != nil {
			return err
		}
		if count >= model.MaxDebtsPerUser {
			return errDebtsLimit
		}
		if err := s.debts.Insert(ctx, debt); err != nil {
			return err
		}
		return s.ledger.record(ctx, debtLifecycle(model.MovementOpening, debt, now))
	})
	if err != nil {
		return inbound.DebtView{}, err
	}
	return s.view(debt), nil
}

// debtLifecycle is a debt being added (OPENING) or removed (CLOSING), with its balance then.
func debtLifecycle(kind model.MovementKind, d model.Debt, now time.Time) model.Movement {
	ref := model.DebtRefOf(d)
	return model.Movement{
		Id: model.NewMovementId(), UserId: d.UserId, Kind: kind, OccurredAt: now,
		Amount: d.Balance, Fee: model.ZeroMoney, Debt: &ref, CreatedAt: now,
	}
}

// debtEdit is what an UpdateDebtCommand changes, validated.
type debtEdit struct {
	name, lender, notes *string
	kind                *model.DebtKind
	balance             *model.Money
	rate                inbound.Change[decimal.Decimal]
	payment             inbound.Change[model.Money]
	dueDay              inbound.Change[int]
	reason              model.BalanceChangeReason
	note                string
	occurredAt          time.Time
}

func validateDebtEdit(cmd inbound.UpdateDebtCommand, now time.Time) (debtEdit, error) {
	var e debtEdit
	if cmd.Name != nil {
		name, err := model.NormalizeDebtName(*cmd.Name)
		if err != nil {
			return e, err
		}
		e.name = &name
	}
	if cmd.Lender != nil {
		lender, err := model.NormalizeDebtLender(*cmd.Lender)
		if err != nil {
			return e, err
		}
		e.lender = &lender
	}
	if cmd.Kind != nil {
		kind, err := model.ParseDebtKind(*cmd.Kind)
		if err != nil {
			return e, err
		}
		e.kind = &kind
	}
	if cmd.BalanceUsd != nil {
		balance, err := model.NewMoneyFromFloat(*cmd.BalanceUsd)
		if err != nil {
			return e, err
		}
		e.balance = &balance
	}
	if c := cmd.InterestRatePct; c.Set {
		rate, err := interestRate(c.Value)
		if err != nil {
			return e, err
		}
		e.rate = inbound.Change[decimal.Decimal]{Set: true, Value: rate}
	}
	if c := cmd.MonthlyPaymentUsd; c.Set {
		payment, err := monthlyPayment(c.Value)
		if err != nil {
			return e, err
		}
		e.payment = inbound.Change[model.Money]{Set: true, Value: payment}
	}
	if c := cmd.DueDay; c.Set {
		day, err := dueDay(c.Value)
		if err != nil {
			return e, err
		}
		e.dueDay = inbound.Change[int]{Set: true, Value: day}
	}
	if cmd.Notes != nil {
		notes, err := model.NormalizeDebtNotes(*cmd.Notes)
		if err != nil {
			return e, err
		}
		e.notes = &notes
	}
	reason, err := model.ParseBalanceChangeReason(cmd.BalanceChangeReason)
	if err != nil {
		return e, err
	}
	e.reason = reason
	if e.note, err = model.NormalizeNote(cmd.Note); err != nil {
		return e, err
	}
	e.occurredAt = now
	if cmd.OccurredAt != nil {
		e.occurredAt = cmd.OccurredAt.UTC()
	}
	return e, model.CheckOccurredAt(e.occurredAt, now)
}

// apply is the debt with the edit's changes.
func (e debtEdit) apply(d model.Debt) model.Debt {
	if e.name != nil {
		d.Name = *e.name
	}
	if e.lender != nil {
		d.Lender = *e.lender
	}
	if e.kind != nil {
		d.Kind = *e.kind
	}
	if e.balance != nil {
		d.Balance = *e.balance
	}
	if e.rate.Set {
		d.InterestRatePct = e.rate.Value
	}
	if e.payment.Set {
		d.MonthlyPayment = e.payment.Value
	}
	if e.dueDay.Set {
		d.DueDay = e.dueDay.Value
	}
	if e.notes != nil {
		d.Notes = *e.notes
	}
	return d
}

func sameDecimal(a, b *decimal.Decimal) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Equal(*b))
}

func sameMoney(a, b *model.Money) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Cmp(*b) == 0)
}

func sameInt(a, b *int) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// sameDebt says whether an edit changed nothing.
func sameDebt(a, b model.Debt) bool {
	return a.Name == b.Name && a.Lender == b.Lender && a.Kind == b.Kind && a.Balance.Cmp(b.Balance) == 0 &&
		sameDecimal(a.InterestRatePct, b.InterestRatePct) && sameMoney(a.MonthlyPayment, b.MonthlyPayment) &&
		sameInt(a.DueDay, b.DueDay) && a.Notes == b.Notes
}

// UpdateDebt changes what the command sets, validated like CreateDebt before anything is read. A new
// balance is recorded as a movement, computed against the balance read in the same transaction. Sending
// what's already there writes nothing and keeps UpdatedAt.
func (s *DebtService) UpdateDebt(ctx context.Context, cmd inbound.UpdateDebtCommand) (inbound.DebtView, error) {
	now := s.clock()
	edit, err := validateDebtEdit(cmd, now)
	if err != nil {
		return inbound.DebtView{}, err
	}

	var result model.Debt
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		current, err := findDebt(ctx, s.debts, cmd.UserId, cmd.Id)
		if err != nil {
			return err
		}
		updated := edit.apply(current)
		if sameDebt(updated, current) {
			result = current
			return nil
		}
		previous, next := current.Balance, updated.Balance
		balanceChanged := previous.Cmp(next) != 0
		var kind model.MovementKind
		if balanceChanged {
			if kind, err = edit.reason.KindFor(previous, next); err != nil {
				return err
			}
		}
		updated.UpdatedAt = now
		if err := updateDebt(ctx, s.debts, updated); err != nil {
			return err
		}
		result = updated
		if !balanceChanged {
			return nil
		}
		ref := model.DebtRefOf(updated)
		return s.ledger.record(ctx, model.Movement{
			Id: model.NewMovementId(), UserId: cmd.UserId, Kind: kind, OccurredAt: edit.occurredAt,
			Amount: model.MustMoney(next.Amount().Sub(previous.Amount()).Abs()), Fee: model.ZeroMoney, Debt: &ref,
			PreviousValue: &previous, NewValue: &next, Note: edit.note, CreatedAt: now,
		})
	})
	if err != nil {
		return inbound.DebtView{}, err
	}
	return s.view(result), nil
}

// DeleteDebt removes the debt and records its CLOSING, with the balance it had, in one transaction.
func (s *DebtService) DeleteDebt(ctx context.Context, userId model.UserId, id model.DebtId) error {
	now := s.clock()
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		d, err := findDebt(ctx, s.debts, userId, id)
		if err != nil {
			return err
		}
		deleted, err := s.debts.DeleteById(ctx, userId, id)
		if err != nil {
			return err
		}
		if !deleted {
			return debtNotFound(id)
		}
		return s.ledger.record(ctx, debtLifecycle(model.MovementClosing, d, now))
	})
}

func debtNotFound(id model.DebtId) error {
	return appErrors.NewResourceNotFoundError(fmt.Sprintf("Debt %s not found", id.String()))
}

// findDebt is the user's debt, or the not-found error the API answers with.
func findDebt(ctx context.Context, repo outbound.DebtRepository, userId model.UserId, id model.DebtId) (model.Debt, error) {
	d, err := repo.FindById(ctx, userId, id)
	if err != nil {
		return model.Debt{}, err
	}
	if d == nil {
		return model.Debt{}, debtNotFound(id)
	}
	return *d, nil
}

// updateDebt stores d, which was read in this transaction: gone since (or someone else's) is not found.
func updateDebt(ctx context.Context, repo outbound.DebtRepository, d model.Debt) error {
	found, err := repo.Update(ctx, d)
	if err != nil {
		return err
	}
	if !found {
		return debtNotFound(d.Id)
	}
	return nil
}

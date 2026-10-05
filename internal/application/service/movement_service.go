package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/parallel"
)

// Why each kind can't take a holding below zero, after "<name> is worth <value>: ".
var whyNotBelowZero = map[model.MovementKind]string{
	model.MovementLoss:        "a loss can't be larger than that.",
	model.MovementWithdrawal:  "you can't withdraw more than that.",
	model.MovementTransfer:    "you can't transfer more than that.",
	model.MovementDebtPayment: "you can't pay more than that.",
}

type MovementService struct {
	tx        outbound.TransactionManager
	holdings  outbound.HoldingRepository
	platforms outbound.PlatformRepository
	debts     outbound.DebtRepository
	movements outbound.MovementRepository
	ledger    ledger
	clock     Clock
}

func NewMovementService(
	tx outbound.TransactionManager,
	holdings outbound.HoldingRepository,
	platforms outbound.PlatformRepository,
	debts outbound.DebtRepository,
	movements outbound.MovementRepository,
	quotas outbound.QuotaRepository,
	clock Clock,
) *MovementService {
	if clock == nil {
		clock = RealClock
	}
	return &MovementService{
		tx:        tx,
		holdings:  holdings,
		platforms: platforms,
		debts:     debts,
		movements: movements,
		ledger:    ledger{movements: movements, quotas: quotas},
		clock:     clock,
	}
}

var _ inbound.MovementUseCase = (*MovementService)(nil)

// RecordMovement validates everything it can before touching storage, then reads the holdings, applies the
// movement to their values and stores it, in one transaction.
func (s *MovementService) RecordMovement(ctx context.Context, cmd inbound.RecordMovementCommand) (inbound.MovementView, error) {
	now := s.clock()
	kind, err := model.ParseMovementKind(cmd.Kind)
	if err != nil || !kind.IsRecordable() {
		return inbound.MovementView{}, appErrors.NewValidationErrors([]appErrors.ValidationError{
			{Field: "kind", Message: "kind must be one of GAIN, LOSS, DEPOSIT, WITHDRAWAL, TRANSFER, DEBT_PAYMENT, DEBT_CHARGE, DEBT_INTEREST"},
		})
	}
	amount, err := model.PositiveAmount(cmd.AmountUsd)
	if err != nil {
		return inbound.MovementView{}, err
	}
	note, err := model.NormalizeNote(cmd.Note)
	if err != nil {
		return inbound.MovementView{}, err
	}
	occurredAt := now
	if cmd.OccurredAt != nil {
		occurredAt = cmd.OccurredAt.UTC()
	}
	if err := model.CheckOccurredAt(occurredAt, now); err != nil {
		return inbound.MovementView{}, err
	}
	movement := model.Movement{
		Id: model.NewMovementId(), UserId: cmd.UserId, Kind: kind, OccurredAt: occurredAt,
		Amount: amount, Fee: model.ZeroMoney, Note: note, CreatedAt: now,
	}

	if kind == model.MovementTransfer {
		return s.transfer(ctx, cmd, movement)
	}
	if kind.IsDebtKind() {
		return s.debtMovement(ctx, cmd, movement)
	}
	if cmd.HoldingId == nil {
		return inbound.MovementView{}, required("holdingId", "holdingId is required")
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := findHolding(ctx, s.holdings, cmd.UserId, *cmd.HoldingId)
		if err != nil {
			return err
		}
		ref := model.RefOf(h)
		movement.Holding = &ref
		updated, err := h.WithDelta(movement.Effect()[0].Delta)
		if errors.Is(err, model.ErrNegativeBalance) {
			return insufficient(h, whyNotBelowZero[kind])
		}
		updated.UpdatedAt = now
		if err := updateHolding(ctx, s.holdings, updated); err != nil {
			return err
		}
		return s.ledger.record(ctx, movement)
	})
	if err != nil {
		return inbound.MovementView{}, err
	}
	return inbound.MovementView{Movement: movement, HoldingExists: true, Revertible: true}, nil
}

func required(field, message string) error {
	return appErrors.NewValidationErrors([]appErrors.ValidationError{{Field: field, Message: message}})
}

// transfer moves the amount from one holding to another (an existing one, or one it creates on the way);
// the destination gets the amount minus the fee.
func (s *MovementService) transfer(ctx context.Context, cmd inbound.RecordMovementCommand, movement model.Movement) (inbound.MovementView, error) {
	var problems []appErrors.ValidationError
	if cmd.FromHoldingId == nil {
		problems = append(problems, appErrors.ValidationError{Field: "fromHoldingId", Message: "fromHoldingId is required"})
	}
	if (cmd.ToHoldingId == nil) == (cmd.ToNewHolding == nil) {
		problems = append(problems, appErrors.ValidationError{Field: "toHoldingId", Message: "Send either toHoldingId or toNewHolding"})
	}
	if cmd.FromHoldingId != nil && cmd.ToHoldingId != nil && *cmd.FromHoldingId == *cmd.ToHoldingId {
		problems = append(problems, appErrors.ValidationError{Field: "toHoldingId", Message: "Pick a different destination"})
	}
	if len(problems) > 0 {
		return inbound.MovementView{}, appErrors.NewValidationErrors(problems)
	}
	fee, err := model.TransferFee(cmd.FeeUsd, movement.Amount)
	if err != nil {
		return inbound.MovementView{}, err
	}
	movement.Fee = fee

	// A new destination is validated like any new holding, before anything is read.
	var fresh model.Holding
	var freshPlatform model.PlatformName
	if n := cmd.ToNewHolding; n != nil {
		if freshPlatform, err = model.NewPlatformName(n.Platform); err != nil {
			return inbound.MovementView{}, err
		}
		ac, err := model.NewAssetClass(n.AssetClass)
		if err != nil {
			return inbound.MovementView{}, err
		}
		if fresh, err = model.CreateHolding(cmd.UserId, n.Name, ac, freshPlatform, model.ZeroMoney, movement.CreatedAt); err != nil {
			return inbound.MovementView{}, err
		}
	}

	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		from, err := findHolding(ctx, s.holdings, cmd.UserId, *cmd.FromHoldingId)
		if err != nil {
			return err
		}
		var to model.Holding
		if cmd.ToHoldingId != nil {
			if to, err = findHolding(ctx, s.holdings, cmd.UserId, *cmd.ToHoldingId); err != nil {
				return err
			}
		} else {
			if err := s.underHoldingsCap(ctx, cmd.UserId); err != nil {
				return err
			}
			to = fresh
			if to.Platform, err = s.platforms.Canonical(ctx, cmd.UserId, freshPlatform); err != nil {
				return err
			}
		}

		fromRef, toRef := model.RefOf(from), model.RefOf(to)
		movement.Holding, movement.ToHolding = &fromRef, &toRef
		effect := movement.Effect()
		fromAfter, err := from.WithDelta(effect[0].Delta)
		if errors.Is(err, model.ErrNegativeBalance) {
			return insufficient(from, whyNotBelowZero[model.MovementTransfer])
		}
		toAfter, _ := to.WithDelta(effect[1].Delta) // amount − fee ≥ 0: it never goes below zero
		fromAfter.UpdatedAt, toAfter.UpdatedAt = movement.CreatedAt, movement.CreatedAt
		if err := updateHolding(ctx, s.holdings, fromAfter); err != nil {
			return err
		}
		if cmd.ToHoldingId != nil {
			err = updateHolding(ctx, s.holdings, toAfter)
		} else {
			_, err = s.holdings.Save(ctx, toAfter)
		}
		if err != nil {
			return err
		}
		return s.ledger.record(ctx, movement)
	})
	if err != nil {
		return inbound.MovementView{}, err
	}
	return inbound.MovementView{Movement: movement, HoldingExists: true, ToHoldingExists: true, Revertible: true}, nil
}

// debtMovement records a debt's payment, charge or interest: the debt's balance changes, and so does the
// holding the money came from (a payment) or went to (a charge), if one is named, in one transaction.
func (s *MovementService) debtMovement(ctx context.Context, cmd inbound.RecordMovementCommand, movement model.Movement) (inbound.MovementView, error) {
	if cmd.DebtId == nil {
		return inbound.MovementView{}, required("debtId", "debtId is required")
	}
	var holdingId *model.HoldingId
	switch movement.Kind {
	case model.MovementDebtPayment:
		holdingId = cmd.FromHoldingId
	case model.MovementDebtCharge:
		holdingId = cmd.ToHoldingId
	}

	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		d, err := findDebt(ctx, s.debts, cmd.UserId, *cmd.DebtId)
		if err != nil {
			return err
		}
		debtRef := model.DebtRefOf(d)
		movement.Debt, movement.Holding, movement.ToHolding = &debtRef, nil, nil
		var h model.Holding
		if holdingId != nil {
			if h, err = findHolding(ctx, s.holdings, cmd.UserId, *holdingId); err != nil {
				return err
			}
			ref := model.RefOf(h)
			if movement.Kind == model.MovementDebtPayment {
				movement.Holding = &ref
			} else {
				movement.ToHolding = &ref
			}
		}

		delta, _ := movement.DebtEffect()
		updated, err := d.WithDelta(delta)
		if errors.Is(err, model.ErrNegativeBalance) {
			return appErrors.NewInsufficientBalanceError(fmt.Sprintf("%s only has %s left to pay.", d.Name, d.Balance.USD()))
		}
		updated.UpdatedAt = movement.CreatedAt
		if err := updateDebt(ctx, s.debts, updated); err != nil {
			return err
		}
		for _, change := range movement.Effect() {
			after, err := h.WithDelta(change.Delta)
			if errors.Is(err, model.ErrNegativeBalance) {
				return insufficient(h, whyNotBelowZero[movement.Kind])
			}
			after.UpdatedAt = movement.CreatedAt
			if err := updateHolding(ctx, s.holdings, after); err != nil {
				return err
			}
		}
		return s.ledger.record(ctx, movement)
	})
	if err != nil {
		return inbound.MovementView{}, err
	}
	return inbound.MovementView{
		Movement: movement, HoldingExists: movement.Holding != nil, ToHoldingExists: movement.ToHolding != nil,
		DebtExists: true, Revertible: true,
	}, nil
}

// underHoldingsCap is the holdings cap checked inside the transaction that adds one. The transaction also
// counts a movement (ledger.record), which serializes it with the user's other changes: the count is exact.
func (s *MovementService) underHoldingsCap(ctx context.Context, userId model.UserId) error {
	return holdingsUnderCap(ctx, s.holdings, userId)
}

func holdingsUnderCap(ctx context.Context, repo outbound.HoldingRepository, userId model.UserId) error {
	count, err := repo.Count(ctx, userId)
	if err != nil {
		return err
	}
	if count >= model.MaxHoldingsPerUser {
		return errHoldingsLimit
	}
	return nil
}

func (s *MovementService) ListMovements(ctx context.Context, userId model.UserId, query inbound.MovementQuery) (inbound.MovementList, error) {
	page, err := s.movements.List(ctx, userId, query.Filter, query.After, query.Limit)
	if err != nil {
		return inbound.MovementList{}, err
	}
	var ids []model.HoldingId
	var debtIds []model.DebtId
	for _, m := range page.Items {
		for _, ref := range []*model.HoldingRef{m.Holding, m.ToHolding} {
			if ref != nil {
				ids = append(ids, ref.Id)
			}
		}
		if m.Debt != nil {
			debtIds = append(debtIds, m.Debt.Id)
		}
	}
	var existing map[model.HoldingId]bool
	var existingDebts map[model.DebtId]bool
	err = parallel.Run(ctx,
		func(ctx context.Context) (err error) {
			existing, err = s.holdings.ExistingIds(ctx, userId, ids)
			return err
		},
		func(ctx context.Context) (err error) {
			existingDebts, err = s.debts.ExistingIds(ctx, userId, debtIds)
			return err
		},
	)
	if err != nil {
		return inbound.MovementList{}, err
	}

	list := inbound.MovementList{Items: make([]inbound.MovementView, len(page.Items)), Next: page.Next}
	for i, m := range page.Items {
		view := inbound.MovementView{Movement: m, Revertible: m.Kind.Revertible()}
		if m.Holding != nil {
			view.HoldingExists = existing[m.Holding.Id]
			view.Revertible = view.Revertible && view.HoldingExists
		}
		if m.ToHolding != nil {
			view.ToHoldingExists = existing[m.ToHolding.Id]
			view.Revertible = view.Revertible && view.ToHoldingExists
		}
		if m.Debt != nil {
			view.DebtExists = existingDebts[m.Debt.Id]
			view.Revertible = view.Revertible && view.DebtExists
		}
		list.Items[i] = view
	}
	return list, nil
}

func (s *MovementService) RevertMovement(ctx context.Context, userId model.UserId, id model.MovementId) error {
	now := s.clock()
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		m, err := s.movements.FindById(ctx, userId, id)
		if err != nil {
			return err
		}
		if m == nil {
			return movementNotFound(id)
		}
		if !m.Kind.Revertible() {
			what := "an asset"
			if m.Debt != nil {
				what = "a debt"
			}
			return appErrors.NewNotRevertibleError(fmt.Sprintf(
				"Adding or removing %s can't be undone here: remove it, or add it again.", what))
		}
		for _, change := range m.Effect() {
			h, err := s.holdings.FindById(ctx, userId, change.Holding)
			if err != nil {
				return err
			}
			if h == nil {
				return appErrors.NewNotRevertibleError(fmt.Sprintf("%s was removed, so this can't be undone.", nameOf(*m, change.Holding)))
			}
			reverted, err := h.WithDelta(change.Delta.Neg())
			if errors.Is(err, model.ErrNegativeBalance) {
				return insufficient(*h, "undoing this would take it below zero.")
			}
			reverted.UpdatedAt = now
			if err := updateHolding(ctx, s.holdings, reverted); err != nil {
				return err
			}
		}
		if delta, ok := m.DebtEffect(); ok {
			d, err := s.debts.FindById(ctx, userId, m.Debt.Id)
			if err != nil {
				return err
			}
			if d == nil {
				return appErrors.NewNotRevertibleError(fmt.Sprintf("%s was removed, so this can't be undone.", m.Debt.Name))
			}
			reverted, err := d.WithDelta(delta.Neg())
			if errors.Is(err, model.ErrNegativeBalance) {
				return appErrors.NewInsufficientBalanceError(fmt.Sprintf(
					"%s has %s left to pay: undoing this would take it below zero.", d.Name, d.Balance.USD()))
			}
			reverted.UpdatedAt = now
			if err := updateDebt(ctx, s.debts, reverted); err != nil {
				return err
			}
		}
		return s.ledger.forget(ctx, *m)
	})
}

// nameOf is how the movement remembers one of its holdings.
func nameOf(m model.Movement, id model.HoldingId) string {
	if m.ToHolding != nil && m.ToHolding.Id == id {
		return m.ToHolding.Name
	}
	return m.Holding.Name
}

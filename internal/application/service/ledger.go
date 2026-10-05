package service

import (
	"context"
	"fmt"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
)

const movementsQuota = "movements"

var errMovementsLimit = appErrors.NewLimitExceededError(fmt.Sprintf(
	"You've reached the limit of %d recorded changes. Undo some to record new ones.", model.MaxMovementsPerUser))

// ledger writes the activity log inside the caller's transaction, counting each movement against the
// user's quota. Every count writes the user's counter, so it also serializes a user's concurrent changes:
// a cap checked in the same transaction (holdings) is exact.
type ledger struct {
	movements outbound.MovementRepository
	quotas    outbound.QuotaRepository
}

// record stores m. A CLOSING isn't counted: removing a holding always works, even with a full quota (each
// one closes an opening that was counted, so they can't grow the log on their own).
func (l ledger) record(ctx context.Context, m model.Movement) error {
	if m.Kind != model.MovementClosing {
		ok, err := l.quotas.Reserve(ctx, m.UserId, movementsQuota, 1, model.MaxMovementsPerUser)
		if err != nil {
			return err
		}
		if !ok {
			return errMovementsLimit
		}
	}
	return l.movements.Save(ctx, m)
}

// forget deletes m and gives its place in the quota back.
func (l ledger) forget(ctx context.Context, m model.Movement) error {
	deleted, err := l.movements.DeleteById(ctx, m.UserId, m.Id)
	if err != nil {
		return err
	}
	if !deleted {
		return movementNotFound(m.Id)
	}
	if m.Kind == model.MovementClosing {
		return nil
	}
	return l.quotas.Release(ctx, m.UserId, movementsQuota, 1)
}

func movementNotFound(id model.MovementId) error {
	return appErrors.NewResourceNotFoundError(fmt.Sprintf("Movement %s not found", id.String()))
}

// insufficient says why a change that would take h below zero was refused.
func insufficient(h model.Holding, why string) error {
	return appErrors.NewInsufficientBalanceError(fmt.Sprintf("%s is worth %s: %s", h.Name, h.Value.USD(), why))
}

// findHolding is the user's holding, or the not-found error the API answers with.
func findHolding(ctx context.Context, repo outbound.HoldingRepository, userId model.UserId, id model.HoldingId) (model.Holding, error) {
	h, err := repo.FindById(ctx, userId, id)
	if err != nil {
		return model.Holding{}, err
	}
	if h == nil {
		return model.Holding{}, holdingNotFound(id)
	}
	return *h, nil
}

// updateHolding stores h, which was read in this transaction: gone since (or someone else's) is not found.
func updateHolding(ctx context.Context, repo outbound.HoldingRepository, h model.Holding) error {
	found, err := repo.Update(ctx, h)
	if err != nil {
		return err
	}
	if !found {
		return holdingNotFound(h.Id)
	}
	return nil
}

package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type DebtRepository interface {
	// FindAll is the user's debts, largest balance first, then by name.
	FindAll(ctx context.Context, userId model.UserId) ([]model.Debt, error)
	// FindById is the user's debt with that id, or nil if there's none (someone else's included).
	FindById(ctx context.Context, userId model.UserId, id model.DebtId) (*model.Debt, error)
	Count(ctx context.Context, userId model.UserId) (int64, error)
	// Insert stores a new debt.
	Insert(ctx context.Context, debt model.Debt) error
	// Update replaces a debt that still exists; false if it's gone (or never was the user's), so an edit
	// racing a delete can't bring the debt back.
	Update(ctx context.Context, debt model.Debt) (bool, error)
	DeleteById(ctx context.Context, userId model.UserId, id model.DebtId) (bool, error)
	// ExistingIds says which of these debts the user still has.
	ExistingIds(ctx context.Context, userId model.UserId, ids []model.DebtId) (map[model.DebtId]bool, error)
}

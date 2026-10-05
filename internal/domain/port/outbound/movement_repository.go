package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type MovementPage struct {
	Items []model.Movement
	Next  *model.MovementCursor // nil on the last page
}

type MovementRepository interface {
	Save(ctx context.Context, movement model.Movement) error
	// FindById is the user's movement with that id, or nil if there's none (someone else's included).
	FindById(ctx context.Context, userId model.UserId, id model.MovementId) (*model.Movement, error)
	List(ctx context.Context, userId model.UserId, filter model.MovementFilter, after *model.MovementCursor, limit int) (MovementPage, error)
	DeleteById(ctx context.Context, userId model.UserId, id model.MovementId) (bool, error)
}

package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type HoldingRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.Holding, error)
	// FindById is the user's holding with that id, or nil if there's none (someone else's included).
	FindById(ctx context.Context, userId model.UserId, id model.HoldingId) (*model.Holding, error)
	Count(ctx context.Context, userId model.UserId) (int64, error)
	Save(ctx context.Context, holding model.Holding) (model.Holding, error)
	// Update replaces a holding that still exists; false if it's gone (or never was the user's), so an
	// edit racing a delete can't bring the holding back.
	Update(ctx context.Context, holding model.Holding) (bool, error)
	DeleteById(ctx context.Context, userId model.UserId, id model.HoldingId) (bool, error)
	AssetClassesInUse(ctx context.Context, userId model.UserId) ([]model.AssetClass, error)
}

package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type HoldingRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.Holding, error)
	Count(ctx context.Context, userId model.UserId) (int64, error)
	Save(ctx context.Context, holding model.Holding) (model.Holding, error)
	DeleteById(ctx context.Context, userId model.UserId, id model.HoldingId) (bool, error)
	AssetClassesInUse(ctx context.Context, userId model.UserId) ([]model.AssetClass, error)
}

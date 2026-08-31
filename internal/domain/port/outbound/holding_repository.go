package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type HoldingRepository interface {
	FindAll(ctx context.Context, userId model.UserId, assetClass *model.AssetClass, platform *model.PlatformName) ([]model.Holding, error)
	FindById(ctx context.Context, userId model.UserId, id model.HoldingId) (*model.Holding, error)
	Save(ctx context.Context, holding model.Holding) (model.Holding, error)
	DeleteById(ctx context.Context, userId model.UserId, id model.HoldingId) (bool, error)
	AssetClassesInUse(ctx context.Context, userId model.UserId) ([]model.AssetClass, error)
}

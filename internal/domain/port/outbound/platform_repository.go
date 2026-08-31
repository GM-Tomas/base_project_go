package outbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type PlatformRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.Platform, error)
	FindByName(ctx context.Context, userId model.UserId, name model.PlatformName) (*model.Platform, error)
	EnsureExists(ctx context.Context, userId model.UserId, name model.PlatformName, now time.Time) (model.PlatformName, error)
	Save(ctx context.Context, platform model.Platform) (model.Platform, error)
	Update(ctx context.Context, userId model.UserId, currentName model.PlatformName, newName *model.PlatformName, newType *model.PlatformType) (*model.Platform, error)
	DeleteByName(ctx context.Context, userId model.UserId, name model.PlatformName) (bool, error)
	CountHoldings(ctx context.Context, userId model.UserId, name model.PlatformName) (int, error)
}

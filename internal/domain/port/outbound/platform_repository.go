package outbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type PlatformRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.Platform, error)
	EnsureExists(ctx context.Context, userId model.UserId, name model.PlatformName, now time.Time) (model.PlatformName, error)
	// DeleteUnused removes the user's platforms that no holding references anymore.
	DeleteUnused(ctx context.Context, userId model.UserId) error
}

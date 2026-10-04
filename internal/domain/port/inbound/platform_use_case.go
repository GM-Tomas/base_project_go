package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// PlatformUseCase is read-only: platforms are created implicitly when a holding references a new
// one, and pruned when their last holding is deleted (see HoldingService).
type PlatformUseCase interface {
	GetAllPlatforms(ctx context.Context, userId model.UserId) ([]model.Platform, error)
}

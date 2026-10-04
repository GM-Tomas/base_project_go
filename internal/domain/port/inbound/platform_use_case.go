package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// PlatformUseCase is read-only: a platform is a name the user's holdings use, so it appears with the
// first holding on it and is gone with the last.
type PlatformUseCase interface {
	GetAllPlatforms(ctx context.Context, userId model.UserId) ([]model.Platform, error)
}

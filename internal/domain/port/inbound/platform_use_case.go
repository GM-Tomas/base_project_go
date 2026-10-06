package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// UpdatePlatformCommand changes how a platform looks (Type, AvatarText, Color: null is the default) and
// its Name, on all its holdings. A name another platform has (case aside) is a merge into it, only done
// with MergeIfExists (the platform merged into keeps its look).
type UpdatePlatformCommand struct {
	UserId        model.UserId
	Id            string
	Name          *string
	Type          Change[string]
	AvatarText    Change[string]
	Color         Change[string]
	MergeIfExists bool
}

// PlatformUseCase: a platform is a name the user's holdings use, so it appears with the first holding on it
// and is gone with the last; how the user set it up stays for when it's used again.
type PlatformUseCase interface {
	GetAllPlatforms(ctx context.Context, userId model.UserId) ([]model.Platform, error)
	UpdatePlatform(ctx context.Context, command UpdatePlatformCommand) (model.Platform, error)
}

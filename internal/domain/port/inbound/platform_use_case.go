package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type CreatePlatformCommand struct {
	UserId model.UserId
	Name   string
	Type   string
}

type PatchPlatformCommand struct {
	UserId  model.UserId
	Name    string
	NewName *string
	NewType *string
}

type PlatformUseCase interface {
	GetAllPlatforms(ctx context.Context, userId model.UserId) ([]model.Platform, error)
	CreatePlatform(ctx context.Context, command CreatePlatformCommand) (model.Platform, error)
	PatchPlatform(ctx context.Context, command PatchPlatformCommand) (model.Platform, error)
	DeletePlatform(ctx context.Context, userId model.UserId, name string) error
}

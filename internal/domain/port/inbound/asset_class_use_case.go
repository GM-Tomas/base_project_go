package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type AvailableAssetClasses struct {
	Defaults []string
	InUse    []string
	All      []string
}

type AssetClassUseCase interface {
	GetAvailableAssetClasses(ctx context.Context, userId model.UserId) (AvailableAssetClasses, error)
}

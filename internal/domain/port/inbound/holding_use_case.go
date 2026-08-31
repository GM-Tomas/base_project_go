package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type CreateHoldingCommand struct {
	UserId     model.UserId
	Name       string
	AssetClass string
	Platform   string
	ValueUsd   float64
}

type PatchHoldingCommand struct {
	UserId     model.UserId
	Id         model.HoldingId
	Name       *string
	AssetClass *string
	Platform   *string
	ValueUsd   *float64
}

type HoldingUseCase interface {
	GetAllHoldings(ctx context.Context, userId model.UserId, assetClass *model.AssetClass, platform *model.PlatformName) ([]model.Holding, error)
	GetHoldingById(ctx context.Context, userId model.UserId, id model.HoldingId) (model.Holding, error)
	CreateHolding(ctx context.Context, command CreateHoldingCommand) (model.Holding, error)
	UpdateHolding(ctx context.Context, command PatchHoldingCommand) (model.Holding, error)
	DeleteHolding(ctx context.Context, userId model.UserId, id model.HoldingId) error
}

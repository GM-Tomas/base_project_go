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

// UpdateHoldingCommand changes the fields that aren't nil and leaves the rest as they are.
type UpdateHoldingCommand struct {
	UserId     model.UserId
	Id         model.HoldingId
	Name       *string
	AssetClass *string
	Platform   *string
	ValueUsd   *float64
}

type HoldingUseCase interface {
	GetAllHoldings(ctx context.Context, userId model.UserId) ([]model.Holding, error)
	CreateHolding(ctx context.Context, command CreateHoldingCommand) (model.Holding, error)
	UpdateHolding(ctx context.Context, command UpdateHoldingCommand) (model.Holding, error)
	DeleteHolding(ctx context.Context, userId model.UserId, id model.HoldingId) error
}

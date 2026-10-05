package inbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type CreateHoldingCommand struct {
	UserId     model.UserId
	Name       string
	AssetClass string
	Platform   string
	ValueUsd   float64
	// ExpectedReturnPct is roughly how much it grows in a year, if known.
	ExpectedReturnPct *float64
}

// UpdateHoldingCommand changes the fields that aren't nil and leaves the rest as they are. A change of value
// is recorded as a movement: ValueChangeReason says which ("" is a market move), OccurredAt when (nil: now).
type UpdateHoldingCommand struct {
	UserId            model.UserId
	Id                model.HoldingId
	Name              *string
	AssetClass        *string
	Platform          *string
	ValueUsd          *float64
	ValueChangeReason string
	OccurredAt        *time.Time
	Note              string
	ExpectedReturnPct Change[float64]
}

// ExpectedReturnItem sets one holding's expected yearly return; nil clears it.
type ExpectedReturnItem struct {
	HoldingId         model.HoldingId
	ExpectedReturnPct *float64
}

type HoldingUseCase interface {
	GetAllHoldings(ctx context.Context, userId model.UserId) ([]model.Holding, error)
	CreateHolding(ctx context.Context, command CreateHoldingCommand) (model.Holding, error)
	UpdateHolding(ctx context.Context, command UpdateHoldingCommand) (model.Holding, error)
	DeleteHolding(ctx context.Context, userId model.UserId, id model.HoldingId) error
	// SetExpectedReturns sets the expected return of many holdings at once: all of them or, if one isn't
	// the user's, none. It returns them, in the order asked.
	SetExpectedReturns(ctx context.Context, userId model.UserId, items []ExpectedReturnItem) ([]model.Holding, error)
}

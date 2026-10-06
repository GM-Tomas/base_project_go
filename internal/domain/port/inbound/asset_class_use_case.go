package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

// AssetClassView is one of the user's classes as they set it up, and what its holdings are worth.
type AssetClassView struct {
	Name model.AssetClass
	// Color is nil for its default one.
	Color             *model.Color
	Liquid            bool
	ExpectedReturnPct *decimal.Decimal
	IsDefault         bool
	HoldingsCount     int
	Value             model.Money
}

// AvailableAssetClasses are the classes every account starts with (Defaults), those the user's holdings use
// (InUse), and the ones the user has (All: the defaults they kept, in order, then the rest by name, as
// Classes lists them).
type AvailableAssetClasses struct {
	Defaults []string
	InUse    []string
	All      []string
	Classes  []AssetClassView
}

type CreateAssetClassCommand struct {
	UserId            model.UserId
	Name              string
	Color             *string
	Liquid            *bool
	ExpectedReturnPct *float64
}

// UpdateAssetClassCommand changes what's set. A new Name renames the class on all its holdings; one the user
// already has is a merge into it, only done with MergeIfExists (the class merged into keeps its settings).
type UpdateAssetClassCommand struct {
	UserId            model.UserId
	Id                string
	Name              *string
	Color             Change[string]
	Liquid            Change[bool]
	ExpectedReturnPct Change[float64]
	MergeIfExists     bool
}

// DeleteAssetClassCommand removes a class; its holdings, if any, move to MoveTo.
type DeleteAssetClassCommand struct {
	UserId model.UserId
	Id     string
	MoveTo *string
}

type AssetClassUseCase interface {
	GetAvailableAssetClasses(ctx context.Context, userId model.UserId) (AvailableAssetClasses, error)
	CreateAssetClass(ctx context.Context, command CreateAssetClassCommand) (AssetClassView, error)
	UpdateAssetClass(ctx context.Context, command UpdateAssetClassCommand) (AssetClassView, error)
	DeleteAssetClass(ctx context.Context, command DeleteAssetClassCommand) error
}

package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type AssetClassAggregate struct {
	AssetClass model.AssetClass
	Value      model.Money
	Count      int
}

type PlatformAggregate struct {
	Name  model.PlatformName
	Type  model.PlatformType
	Value model.Money
	Count int
}

// WealthBreakdown is a user's wealth and how it splits, from one read of their holdings: the three always
// agree, even while the holdings change.
type WealthBreakdown struct {
	NetWorth     model.Money
	ByAssetClass []AssetClassAggregate
	ByPlatform   []PlatformAggregate
}

type WealthAggregationPort interface {
	NetWorth(ctx context.Context, userId model.UserId) (model.Money, error)
	Breakdown(ctx context.Context, userId model.UserId) (WealthBreakdown, error)
}

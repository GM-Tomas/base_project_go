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

type WealthAggregationPort interface {
	NetWorth(ctx context.Context, userId model.UserId) (model.Money, error)
	ByAssetClass(ctx context.Context, userId model.UserId) ([]AssetClassAggregate, error)
	ByPlatform(ctx context.Context, userId model.UserId) ([]PlatformAggregate, error)
}

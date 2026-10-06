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
	Key   string // see model.PlatformKey
	Name  model.PlatformName
	Type  model.PlatformType
	Value model.Money
	Count int
}

// WealthTotals is what a user owns (their holdings) and what they owe (their debts), with each holding's
// value and expected return (the portfolio's expected return weighs them).
type WealthTotals struct {
	Assets  model.Money
	Debts   model.Money
	Returns []model.HoldingReturn
}

// NetWorth is assets minus debts.
func (t WealthTotals) NetWorth() model.SignedMoney {
	return model.NetOf(t.Assets, t.Debts)
}

// WealthBreakdown is a user's wealth and how it splits, from one read of their holdings (the assets and
// both breakdowns always agree, even while the holdings change) and one of their debts.
type WealthBreakdown struct {
	Assets       model.Money
	Debts        model.DebtTotals
	ByAssetClass []AssetClassAggregate
	ByPlatform   []PlatformAggregate
	Returns      []model.HoldingReturn
}

type WealthAggregationPort interface {
	Totals(ctx context.Context, userId model.UserId) (WealthTotals, error)
	Breakdown(ctx context.Context, userId model.UserId) (WealthBreakdown, error)
	// ByAssetClass is just the class breakdown: what each class the user has holdings of is worth.
	ByAssetClass(ctx context.Context, userId model.UserId) ([]AssetClassAggregate, error)
}

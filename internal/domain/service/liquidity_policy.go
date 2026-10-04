package service

import (
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

var (
	hundredDecimal = decimal.NewFromInt(100).Truncate(1)
	zeroPercent    = decimal.Zero.Truncate(1)
)

type LiquidityBreakdown struct {
	LiquidPct   decimal.Decimal
	IlliquidPct decimal.Decimal
}

type LiquidityPolicy struct {
	liquidAssetClasses map[string]struct{}
}

func NewLiquidityPolicy(liquidAssetClasses []model.AssetClass) LiquidityPolicy {
	set := make(map[string]struct{}, len(liquidAssetClasses))
	for _, ac := range liquidAssetClasses {
		set[ac.Value()] = struct{}{}
	}
	return LiquidityPolicy{liquidAssetClasses: set}
}

func (lp LiquidityPolicy) Breakdown(valueByClass map[string]model.Money) LiquidityBreakdown {
	var total model.Money
	var liquidValue model.Money

	for name, val := range valueByClass {
		total = total.Plus(val)
		if _, ok := lp.liquidAssetClasses[name]; ok {
			liquidValue = liquidValue.Plus(val)
		}
	}

	pct := liquidValue.PercentOf(total)
	if pct == nil {
		// Nothing owned: neither liquid nor locked in (the dashboard would otherwise read "100% locked").
		return LiquidityBreakdown{LiquidPct: zeroPercent, IlliquidPct: zeroPercent}
	}
	liquidPct := pct.Round(1)
	illiquidPct := hundredDecimal.Sub(liquidPct).Round(1)

	return LiquidityBreakdown{
		LiquidPct:   liquidPct,
		IlliquidPct: illiquidPct,
	}
}

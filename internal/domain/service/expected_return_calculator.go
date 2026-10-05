package service

import (
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

var hundred = decimal.NewFromInt(100)

// CalculateExpectedReturn is the portfolio's expected yearly return: Σ(valueᵢ × returnᵢ) / Σ valueᵢ over every
// holding, those without a return counting as 0% (cash really does earn nothing; leaving them out would
// overstate it). How much of the portfolio's value has a return says how much of it is estimated.
func CalculateExpectedReturn(holdings []model.HoldingReturn) model.ExpectedReturn {
	total, weighted, covered := decimal.Zero, decimal.Zero, decimal.Zero
	for _, h := range holdings {
		value := h.Value.Amount()
		total = total.Add(value)
		if h.Pct != nil {
			weighted = weighted.Add(value.Mul(*h.Pct))
			covered = covered.Add(value)
		}
	}
	if !total.IsPositive() {
		return model.ExpectedReturn{CoveragePct: decimal.Zero, Annual: model.ZeroSignedMoney}
	}
	pct := weighted.DivRound(total, 2)
	return model.ExpectedReturn{
		WeightedPct: &pct,
		CoveragePct: covered.Mul(hundred).DivRound(total, 1),
		Annual:      model.NewSignedMoney(weighted.Div(hundred)),
	}
}

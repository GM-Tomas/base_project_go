package service

import (
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// CalculateYtdGrowth computes the YTD percentage growth of the net worth against yearStartSnapshot or
// earliestSnapshot. A baseline that isn't above zero has nothing to compare against: no baseline.
func CalculateYtdGrowth(
	currentNetWorth model.SignedMoney,
	yearStartSnapshot *model.NetWorthSnapshot,
	earliestSnapshot *model.NetWorthSnapshot,
) model.YtdGrowth {
	var basis model.YtdBasis
	var baseline *model.NetWorthSnapshot

	if yearStartSnapshot != nil {
		basis = model.YtdBasisYearStartSnapshot
		baseline = yearStartSnapshot
	} else if earliestSnapshot != nil {
		basis = model.YtdBasisEarliestSnapshot
		baseline = earliestSnapshot
	} else {
		return model.NewYtdGrowthNoBaseline()
	}

	growthPct := currentNetWorth.GrowthPctFrom(baseline.TotalValue)
	if growthPct == nil {
		return model.NewYtdGrowthNoBaseline()
	}

	return model.NewYtdGrowthFrom(basis, baseline.TotalValue, baseline.CapturedAt, growthPct.Round(1))
}

package service

import (
	"math"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

const (
	monthsPerYear = 12
	percent       = 100.0
)

// projectedMonth is where a projection stands after a month.
type projectedMonth struct {
	value       float64 // the portfolio
	contributed float64 // the principal and every contribution so far
	deflator    float64 // what a dollar then is worth today: (1 + inflation/12)^month
}

// simulate runs a projection month by month for params.Years*12 months: each month the portfolio earns the
// yield's monthly rate (annual/12), then the month's contribution goes in (an ordinary annuity, so without
// a raise it matches FV = P(1+r)ⁿ + PMT((1+r)ⁿ − 1)/r). The contribution grows by ContributionGrowthPct
// every 12 months. It returns months+1 states, month 0 (the principal) first.
func simulate(params model.ProjectionParams) []projectedMonth {
	months := params.Years * monthsPerYear
	yield, _ := params.AnnualYieldPct.Float64()
	growth, _ := params.ContributionGrowthPct.Float64()
	inflation, _ := params.InflationPct.Float64()
	rate := yield / percent / monthsPerYear
	monthlyInflation := 1 + inflation/percent/monthsPerYear
	contribution := params.MonthlyContribution.Float64()

	states := make([]projectedMonth, months+1)
	value := params.Principal.Float64()
	contributed := value
	deflator := 1.0
	states[0] = projectedMonth{value: value, contributed: contributed, deflator: deflator}
	for m := 1; m <= months; m++ {
		if m > monthsPerYear && (m-1)%monthsPerYear == 0 {
			contribution *= 1 + growth/percent // a new year: the raise
		}
		value = math.Max(0, value*(1+rate)+contribution)
		contributed += contribution
		deflator *= monthlyInflation
		states[m] = projectedMonth{value: value, contributed: contributed, deflator: deflator}
	}
	return states
}

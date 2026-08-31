package service

import (
	"math"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

const (
	monthsPerYear = 12
	percent       = 100.0
	nearZeroRate  = 1e-9
)

// FutureValue calculates FV = P*(1+r)^n + PMT*((1+r)^n - 1)/r, r = annualYieldPct/100/12.
func FutureValue(
	principal model.Money,
	monthlyContribution model.Money,
	annualYieldPct decimal.Decimal,
	months int,
) model.Money {
	p := principal.Float64()
	pmt := monthlyContribution.Float64()
	yieldFloat, _ := annualYieldPct.Float64()
	r := yieldFloat / percent / monthsPerYear

	var fv float64
	if math.Abs(r) < nearZeroRate {
		fv = p + pmt*float64(months)
	} else {
		compound := math.Pow(1.0+r, float64(months))
		fv = p*compound + pmt*((compound-1.0)/r)
	}

	if fv < 0 {
		fv = 0
	}
	return model.MustMoneyFromFloat(fv)
}

// MonthsToReach returns the first month (0..maxMonths) where futureValue >= threshold, or nil if never reached.
func MonthsToReach(
	threshold model.Money,
	principal model.Money,
	monthlyContribution model.Money,
	annualYieldPct decimal.Decimal,
	maxMonths int,
) *int {
	for month := 0; month <= maxMonths; month++ {
		fv := FutureValue(principal, monthlyContribution, annualYieldPct, month)
		if fv.GreaterThanOrEqual(threshold) {
			m := month
			return &m
		}
	}
	return nil
}

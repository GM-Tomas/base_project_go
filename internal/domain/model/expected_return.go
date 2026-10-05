package model

import "github.com/shopspring/decimal"

// HoldingReturn is a holding's value and the yearly return it counts with (see Holding.EffectiveReturnPct),
// if any: what the portfolio's expected return is worked out from.
type HoldingReturn struct {
	Value Money
	Pct   *decimal.Decimal
}

// ExpectedReturn is what the portfolio is expected to earn in a year: each holding's return weighted by its
// value, those without one counting as 0%.
type ExpectedReturn struct {
	// WeightedPct is nil when there's nothing to weigh (no holdings, or all worth 0).
	WeightedPct *decimal.Decimal
	// CoveragePct is the share of the portfolio's value whose return the user set (1 decimal).
	CoveragePct decimal.Decimal
	// Annual is what the portfolio would earn in a year at those returns (negative if it would lose).
	Annual SignedMoney
}

package model

import (
	"time"

	"github.com/shopspring/decimal"
)

type YtdBasis string

const (
	YtdBasisYearStartSnapshot YtdBasis = "YEAR_START_SNAPSHOT"
	YtdBasisEarliestSnapshot  YtdBasis = "EARLIEST_SNAPSHOT"
	YtdBasisNoBaseline        YtdBasis = "NO_BASELINE"
)

// YtdGrowth represents YTD performance calculation result.
type YtdGrowth struct {
	Basis         YtdBasis
	BaselineValue *Money
	BaselineAt    *time.Time
	GrowthPct     decimal.Decimal
}

func NewYtdGrowthFrom(basis YtdBasis, baselineValue Money, baselineAt time.Time, growthPct decimal.Decimal) YtdGrowth {
	return YtdGrowth{
		Basis:         basis,
		BaselineValue: &baselineValue,
		BaselineAt:    &baselineAt,
		GrowthPct:     growthPct,
	}
}

func NewYtdGrowthNoBaseline() YtdGrowth {
	return YtdGrowth{
		Basis:     YtdBasisNoBaseline,
		GrowthPct: decimal.Zero,
	}
}

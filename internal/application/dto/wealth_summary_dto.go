package dto

import "time"

// NetWorthDTO is assets minus debts: below zero when more is owed than owned.
type NetWorthDTO struct {
	Usd float64 `json:"usd"`
}

type AssetsDTO struct {
	Usd float64 `json:"usd"`
}

type DebtsDTO struct {
	Usd               float64 `json:"usd"`
	Count             int     `json:"count"`
	MonthlyPaymentUsd float64 `json:"monthlyPaymentUsd"`
}

// ExpectedReturnDTO is what the portfolio is expected to earn in a year: weightedPct is each holding's return
// weighted by value (null with nothing to weigh), coveragePct the share of the value with a return set,
// annualUsd the dollars.
type ExpectedReturnDTO struct {
	WeightedPct *float64 `json:"weightedPct"`
	CoveragePct float64  `json:"coveragePct"`
	AnnualUsd   float64  `json:"annualUsd"`
}

type YtdDTO struct {
	Basis            string     `json:"basis"`
	GrowthPct        float64    `json:"growthPct"`
	BaselineValueUsd *float64   `json:"baselineValueUsd,omitempty"`
	BaselineAt       *time.Time `json:"baselineAt,omitempty"`
}

type LiquidityDTO struct {
	LiquidPct          float64  `json:"liquidPct"`
	IlliquidPct        float64  `json:"illiquidPct"`
	LiquidAssetClasses []string `json:"liquidAssetClasses"`
}

// AssetClassBreakdown is a class's share: color is the user's for it (null: the default), liquid whether it
// counts as ready to spend.
type AssetClassBreakdown struct {
	AssetClass string  `json:"assetClass"`
	ValueUsd   float64 `json:"valueUsd"`
	Pct        float64 `json:"pct"`
	Count      int     `json:"count"`
	Color      *string `json:"color"`
	Liquid     bool    `json:"liquid"`
}

// PlatformBreakdown is a platform's share: avatarText, color and textColor are its thumbnail's (null: the
// default).
type PlatformBreakdown struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	ValueUsd   float64 `json:"valueUsd"`
	Pct        float64 `json:"pct"`
	Count      int     `json:"count"`
	AvatarText *string `json:"avatarText"`
	Color      *string `json:"color"`
	TextColor  *string `json:"textColor"`
}

type WealthSummaryResponse struct {
	NetWorth       NetWorthDTO           `json:"netWorth"`
	Assets         AssetsDTO             `json:"assets"`
	Debts          DebtsDTO              `json:"debts"`
	HoldingsCount  int                   `json:"holdingsCount"`
	ExpectedReturn ExpectedReturnDTO     `json:"expectedReturn"`
	Ytd            YtdDTO                `json:"ytd"`
	Liquidity      LiquidityDTO          `json:"liquidity"`
	ByAssetClass   []AssetClassBreakdown `json:"byAssetClass"`
	ByPlatform     []PlatformBreakdown   `json:"byPlatform"`
}

package dto

import "time"

type NetWorthDTO struct {
	Usd float64 `json:"usd"`
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

type AssetClassBreakdown struct {
	AssetClass string  `json:"assetClass"`
	ValueUsd   float64 `json:"valueUsd"`
	Pct        float64 `json:"pct"`
	Count      int     `json:"count"`
}

type PlatformBreakdown struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	ValueUsd float64 `json:"valueUsd"`
	Pct      float64 `json:"pct"`
	Count    int     `json:"count"`
}

type WealthSummaryResponse struct {
	NetWorth      NetWorthDTO           `json:"netWorth"`
	HoldingsCount int                   `json:"holdingsCount"`
	Ytd           YtdDTO                `json:"ytd"`
	Liquidity     LiquidityDTO          `json:"liquidity"`
	ByAssetClass  []AssetClassBreakdown `json:"byAssetClass"`
	ByPlatform    []PlatformBreakdown   `json:"byPlatform"`
}

package dto

import "time"

type CreateHoldingRequest struct {
	Name              string   `json:"name"`
	AssetClass        string   `json:"assetClass"`
	Platform          string   `json:"platform"`
	ValueUsd          float64  `json:"valueUsd"`
	ExpectedReturnPct *float64 `json:"expectedReturnPct"`
}

// UpdateHoldingRequest is PATCH /holdings/{id}: only the fields sent change. A new valueUsd is recorded as a
// movement: valueChangeReason says what it was (MARKET by default), occurredAt and note go with it.
type UpdateHoldingRequest struct {
	Name              Optional[string]  `json:"name"`
	AssetClass        Optional[string]  `json:"assetClass"`
	Platform          Optional[string]  `json:"platform"`
	ValueUsd          Optional[float64] `json:"valueUsd"`
	ValueChangeReason string            `json:"valueChangeReason"`
	OccurredAt        string            `json:"occurredAt"`
	Note              string            `json:"note"`
	// ExpectedReturnPct sets the holding's yearly return; null clears it.
	ExpectedReturnPct Optional[float64] `json:"expectedReturnPct"`
}

// ExpectedReturnsRequest is PUT /holdings/expected-returns: each holding's yearly return (null clears it).
type ExpectedReturnsRequest struct {
	Items *[]ExpectedReturnItemRequest `json:"items"`
}

type ExpectedReturnItemRequest struct {
	HoldingId         string            `json:"holdingId"`
	ExpectedReturnPct Optional[float64] `json:"expectedReturnPct"`
}

// HoldingResponse is a holding: expectedReturnPct is its own yearly return (null if not set),
// effectiveReturnPct the one it counts with in the portfolio's.
type HoldingResponse struct {
	Id                 string    `json:"id"`
	Name               string    `json:"name"`
	AssetClass         string    `json:"assetClass"`
	Platform           string    `json:"platform"`
	ValueUsd           float64   `json:"valueUsd"`
	ExpectedReturnPct  *float64  `json:"expectedReturnPct"`
	EffectiveReturnPct *float64  `json:"effectiveReturnPct"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

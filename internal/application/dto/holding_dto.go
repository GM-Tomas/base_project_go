package dto

import "time"

type CreateHoldingRequest struct {
	Name       string  `json:"name"`
	AssetClass string  `json:"assetClass"`
	Platform   string  `json:"platform"`
	ValueUsd   float64 `json:"valueUsd"`
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
}

type HoldingResponse struct {
	Id         string    `json:"id"`
	Name       string    `json:"name"`
	AssetClass string    `json:"assetClass"`
	Platform   string    `json:"platform"`
	ValueUsd   float64   `json:"valueUsd"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

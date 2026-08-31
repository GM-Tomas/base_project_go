package dto

import "time"

type CreateHoldingRequest struct {
	Name       string  `json:"name"`
	AssetClass string  `json:"assetClass"`
	Platform   string  `json:"platform"`
	ValueUsd   float64 `json:"valueUsd"`
}

type UpdateHoldingRequest struct {
	Name       *string  `json:"name,omitempty"`
	AssetClass *string  `json:"assetClass,omitempty"`
	Platform   *string  `json:"platform,omitempty"`
	ValueUsd   *float64 `json:"valueUsd,omitempty"`
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

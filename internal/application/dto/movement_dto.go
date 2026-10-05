package dto

import "time"

// NewHoldingRequest is a holding a transfer creates as its destination.
type NewHoldingRequest struct {
	Name       string `json:"name"`
	AssetClass string `json:"assetClass"`
	Platform   string `json:"platform"`
}

// CreateMovementRequest is POST /movements. Which fields apply depends on kind: holdingId for GAIN, LOSS,
// DEPOSIT and WITHDRAWAL; fromHoldingId, then toHoldingId or toNewHolding (and feeUsd) for TRANSFER.
type CreateMovementRequest struct {
	Kind          string             `json:"kind"`
	HoldingId     string             `json:"holdingId"`
	FromHoldingId string             `json:"fromHoldingId"`
	ToHoldingId   string             `json:"toHoldingId"`
	ToNewHolding  *NewHoldingRequest `json:"toNewHolding"`
	AmountUsd     float64            `json:"amountUsd"`
	FeeUsd        float64            `json:"feeUsd"`
	// OccurredAt is a date (YYYY-MM-DD, kept at noon UTC) or an RFC 3339 instant; empty means now.
	OccurredAt string `json:"occurredAt"`
	Note       string `json:"note"`
}

// MovementHoldingResponse is a holding as the movement remembers it, and whether it still exists.
type MovementHoldingResponse struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	AssetClass string `json:"assetClass"`
	Exists     bool   `json:"exists"`
}

type MovementResponse struct {
	Id               string                   `json:"id"`
	Kind             string                   `json:"kind"`
	OccurredAt       time.Time                `json:"occurredAt"`
	CreatedAt        time.Time                `json:"createdAt"`
	AmountUsd        float64                  `json:"amountUsd"`
	FeeUsd           *float64                 `json:"feeUsd"`
	Holding          *MovementHoldingResponse `json:"holding"`
	ToHolding        *MovementHoldingResponse `json:"toHolding"`
	PreviousValueUsd *float64                 `json:"previousValueUsd"`
	NewValueUsd      *float64                 `json:"newValueUsd"`
	Note             *string                  `json:"note"`
	Revertible       bool                     `json:"revertible"`
}

type MovementListResponse struct {
	Items      []MovementResponse `json:"items"`
	NextCursor *string            `json:"nextCursor"`
}

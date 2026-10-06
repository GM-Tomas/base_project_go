package dto

import "time"

// NewHoldingRequest is a holding a transfer creates as its destination.
type NewHoldingRequest struct {
	Name       string `json:"name"`
	AssetClass string `json:"assetClass"`
	Platform   string `json:"platform"`
}

// CreateMovementRequest is POST /movements. Which fields apply depends on kind: holdingId for GAIN, LOSS,
// DEPOSIT and WITHDRAWAL; fromHoldingId, then toHoldingId or toNewHolding (and feeUsd) for TRANSFER; debtId
// for DEBT_PAYMENT (and fromHoldingId, where the money came from, if any), DEBT_CHARGE (and toHoldingId,
// where it went, if any) and DEBT_INTEREST.
type CreateMovementRequest struct {
	Kind          string             `json:"kind"`
	HoldingId     string             `json:"holdingId"`
	FromHoldingId string             `json:"fromHoldingId"`
	ToHoldingId   string             `json:"toHoldingId"`
	DebtId        string             `json:"debtId"`
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

// MovementDebtResponse is a debt as the movement remembers it, and whether it still exists.
type MovementDebtResponse struct {
	Id     string  `json:"id"`
	Name   string  `json:"name"`
	Lender *string `json:"lender"`
	Exists bool    `json:"exists"`
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
	Debt             *MovementDebtResponse    `json:"debt"`
	PreviousValueUsd *float64                 `json:"previousValueUsd"`
	NewValueUsd      *float64                 `json:"newValueUsd"`
	Note             *string                  `json:"note"`
	Revertible       bool                     `json:"revertible"`
}

type MovementListResponse struct {
	Items      []MovementResponse `json:"items"`
	NextCursor *string            `json:"nextCursor"`
}

// MovementsSummaryResponse is what a period's movements add up to: totalsUsd by bucket (ADJUSTMENT signed,
// as it changed the net worth), and netWorthEffectUsd, the change of net worth they explain, by why.
type MovementsSummaryResponse struct {
	From              time.Time          `json:"from"`
	To                time.Time          `json:"to"`
	Count             int                `json:"count"`
	Transfers         int                `json:"transfers"`
	TotalsUsd         map[string]float64 `json:"totalsUsd"`
	NetWorthEffectUsd NetWorthEffectDTO  `json:"netWorthEffectUsd"`
}

type NetWorthEffectDTO struct {
	Investments  float64 `json:"investments"`
	Saving       float64 `json:"saving"`
	AddedRemoved float64 `json:"addedRemoved"`
	Corrections  float64 `json:"corrections"`
}

package dto

import "time"

type SnapshotResponse struct {
	Id                    string    `json:"id"`
	CapturedAt            time.Time `json:"capturedAt"`
	TotalValueUsd         float64   `json:"totalValueUsd"`
	ChangePctFromPrevious *float64  `json:"changePctFromPrevious,omitempty"`
}

package dto

import "time"

// SnapshotResponse is a net worth at a moment: totalValueUsd is assetsUsd − debtsUsd (below zero when more
// was owed than owned).
type SnapshotResponse struct {
	Id            string    `json:"id"`
	CapturedAt    time.Time `json:"capturedAt"`
	TotalValueUsd float64   `json:"totalValueUsd"`
	AssetsUsd     float64   `json:"assetsUsd"`
	DebtsUsd      float64   `json:"debtsUsd"`
	// ChangePctFromPrevious is null on the first snapshot, and when the previous one wasn't above zero.
	ChangePctFromPrevious *float64 `json:"changePctFromPrevious"`
}

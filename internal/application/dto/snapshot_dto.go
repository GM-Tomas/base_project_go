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
	// Source is AUTO (the net worth when it was taken) or MANUAL (a past one the user entered).
	Source string  `json:"source"`
	Note   *string `json:"note"`
}

// CreateSnapshotRequest is POST /wealth/snapshots' optional body: without it (or with {}), today's net
// worth; with it, a past one. capturedAt and totalValueUsd go together, assetsUsd and debtsUsd too.
type CreateSnapshotRequest struct {
	CapturedAt    string   `json:"capturedAt"`
	TotalValueUsd *float64 `json:"totalValueUsd"`
	AssetsUsd     *float64 `json:"assetsUsd"`
	DebtsUsd      *float64 `json:"debtsUsd"`
	Note          string   `json:"note"`
}

// Empty is whether it asks for today's net worth.
func (r CreateSnapshotRequest) Empty() bool {
	return r.CapturedAt == "" && r.TotalValueUsd == nil && r.AssetsUsd == nil && r.DebtsUsd == nil && r.Note == ""
}

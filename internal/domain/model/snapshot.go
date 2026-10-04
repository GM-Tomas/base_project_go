package model

import (
	"time"
)

// MaxSnapshotsPerUser caps one account's history (over 13 years of daily snapshots).
const MaxSnapshotsPerUser = 5000

// NetWorthSnapshot represents a snapshot of net worth at a specific point in time.
type NetWorthSnapshot struct {
	Id         SnapshotId
	UserId     UserId
	CapturedAt time.Time
	TotalValue Money
}

func NewNetWorthSnapshot(
	id SnapshotId,
	userId UserId,
	capturedAt time.Time,
	totalValue Money,
) NetWorthSnapshot {
	return NetWorthSnapshot{
		Id:         id,
		UserId:     userId,
		CapturedAt: capturedAt,
		TotalValue: totalValue,
	}
}

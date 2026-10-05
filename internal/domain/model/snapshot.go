package model

import (
	"time"
)

// MaxSnapshotsPerUser caps one account's history (over 13 years of daily snapshots).
const MaxSnapshotsPerUser = 5000

// NetWorthSnapshot is a user's net worth at a moment: what they owned, what they owed, and the difference
// (TotalValue, which is below zero when they owed more). Snapshots from before debts existed owed nothing.
type NetWorthSnapshot struct {
	Id         SnapshotId
	UserId     UserId
	CapturedAt time.Time
	Assets     Money
	Debts      Money
	TotalValue SignedMoney
}

func NewNetWorthSnapshot(
	id SnapshotId,
	userId UserId,
	capturedAt time.Time,
	assets Money,
	debts Money,
) NetWorthSnapshot {
	return NetWorthSnapshot{
		Id:         id,
		UserId:     userId,
		CapturedAt: capturedAt,
		Assets:     assets,
		Debts:      debts,
		TotalValue: NetOf(assets, debts),
	}
}

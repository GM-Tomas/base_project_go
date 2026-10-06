package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

const (
	// MaxSnapshotsPerUser caps one account's history (over 13 years of daily snapshots).
	MaxSnapshotsPerUser   = 5000
	MaxSnapshotNoteLength = 200
)

// SnapshotSource is where a snapshot's figures come from.
type SnapshotSource string

const (
	SnapshotAuto   SnapshotSource = "AUTO"   // the net worth when it was taken
	SnapshotManual SnapshotSource = "MANUAL" // one from the past the user entered
)

var (
	ErrCapturedAtOutOfRange    = errors.New("capturedAt must be in the past, from 1970 on")
	ErrSnapshotPartsMismatch   = errors.New("totalValueUsd must be assetsUsd − debtsUsd")
	ErrSnapshotPartsIncomplete = errors.New("assetsUsd and debtsUsd go together: send both or neither")
	earliestCapturedAt         = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
)

// NetWorthSnapshot is a user's net worth at a moment: what they owned, what they owed, and the difference
// (TotalValue, which is below zero when they owed more). Snapshots from before debts existed owed nothing.
type NetWorthSnapshot struct {
	Id         SnapshotId
	UserId     UserId
	CapturedAt time.Time
	Assets     Money
	Debts      Money
	TotalValue SignedMoney
	Source     SnapshotSource
	Note       string
}

// NewManualSnapshot is a past net worth the user enters: when (to the second, not after now), how much and,
// if they know, what they owned and owed, which it must then be the difference of. Without them, it's what
// they owned, or what they owed if it's below zero.
func NewManualSnapshot(
	id SnapshotId,
	userId UserId,
	capturedAt time.Time,
	total decimal.Decimal,
	assets, debts *Money,
	note string,
	now time.Time,
) (NetWorthSnapshot, error) {
	at := capturedAt.UTC().Truncate(time.Second)
	if at.Before(earliestCapturedAt) || at.After(now) {
		return NetWorthSnapshot{}, ErrCapturedAtOutOfRange
	}
	note = strings.TrimSpace(note)
	if n := utf8.RuneCountInString(note); n > MaxSnapshotNoteLength {
		return NetWorthSnapshot{}, fmt.Errorf("Note %w (%d > %d)", ErrLabelTooLong, n, MaxSnapshotNoteLength)
	}
	net := NewSignedMoney(total)
	if (assets == nil) != (debts == nil) {
		return NetWorthSnapshot{}, ErrSnapshotPartsIncomplete
	}
	owned, owed := ZeroMoney, ZeroMoney
	if assets != nil {
		if NetOf(*assets, *debts).Cmp(net) != 0 {
			return NetWorthSnapshot{}, ErrSnapshotPartsMismatch
		}
		owned, owed = *assets, *debts
	} else if net.Amount().IsNegative() {
		owed = MustMoney(net.Amount().Neg())
	} else {
		owned = MustMoney(net.Amount())
	}
	s := NewNetWorthSnapshot(id, userId, at, owned, owed)
	s.Source, s.Note = SnapshotManual, note
	return s, nil
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
		Source:     SnapshotAuto,
	}
}

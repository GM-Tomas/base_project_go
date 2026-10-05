package model

import (
	"errors"
	"fmt"
	"time"
)

// MovementFilter narrows the activity log; the zero value is all of it.
type MovementFilter struct {
	// HoldingId keeps one holding's movements: where it's the holding, or where the money arrived.
	HoldingId *HoldingId
	DebtId    *DebtId        // keeps one debt's movements
	Kinds     []MovementKind // any of these
	From, To  *time.Time     // occurredAt within [From, To]
}

// MovementCursor is where a page of the log ended. The log runs newest first by (OccurredAt, CreatedAt, Id),
// a total order, so the next page starts right after it whatever was recorded in between.
type MovementCursor struct {
	OccurredAt time.Time
	CreatedAt  time.Time
	Id         MovementId
}

// ValueChangeReason is why a holding's value was edited: it decides what the edit is recorded as.
type ValueChangeReason string

const (
	ReasonMarket     ValueChangeReason = "MARKET"     // a gain or a loss
	ReasonCashFlow   ValueChangeReason = "CASH_FLOW"  // a deposit or a withdrawal
	ReasonCorrection ValueChangeReason = "CORRECTION" // an adjustment
)

var ErrUnknownValueChangeReason = errors.New("valueChangeReason must be one of MARKET, CASH_FLOW, CORRECTION")

// ParseValueChangeReason reads a reason; none is a market move.
func ParseValueChangeReason(raw string) (ValueChangeReason, error) {
	switch ValueChangeReason(raw) {
	case "", ReasonMarket:
		return ReasonMarket, nil
	case ReasonCashFlow, ReasonCorrection:
		return ValueChangeReason(raw), nil
	}
	return "", fmt.Errorf("%w (got %q)", ErrUnknownValueChangeReason, raw)
}

// KindFor is what an edit from previous to next is recorded as, for this reason.
func (r ValueChangeReason) KindFor(previous, next Money) MovementKind {
	up := next.Cmp(previous) > 0
	switch {
	case r == ReasonCorrection:
		return MovementAdjustment
	case r == ReasonCashFlow && up:
		return MovementDeposit
	case r == ReasonCashFlow:
		return MovementWithdrawal
	case up:
		return MovementGain
	}
	return MovementLoss
}

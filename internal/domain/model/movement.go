package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// MovementKind is what a movement records. Users record gains, losses, deposits, withdrawals and transfers;
// the rest the system records when holdings are added, removed or edited.
type MovementKind string

const (
	MovementOpening    MovementKind = "OPENING"    // a holding was added, with its first value
	MovementClosing    MovementKind = "CLOSING"    // a holding was removed, with its last value
	MovementGain       MovementKind = "GAIN"       // interest, dividends, a market rise
	MovementLoss       MovementKind = "LOSS"       // a market fall, a cost
	MovementDeposit    MovementKind = "DEPOSIT"    // new money put in
	MovementWithdrawal MovementKind = "WITHDRAWAL" // money taken out
	MovementTransfer   MovementKind = "TRANSFER"   // from one holding to another, maybe with a fee
	MovementAdjustment MovementKind = "ADJUSTMENT" // a correction: neither performance nor money in or out
)

// RecordableKinds are the kinds users record themselves (POST /movements), in the order the API lists them.
var RecordableKinds = []MovementKind{MovementGain, MovementLoss, MovementDeposit, MovementWithdrawal, MovementTransfer}

var allMovementKinds = []MovementKind{
	MovementOpening, MovementClosing, MovementGain, MovementLoss, MovementDeposit, MovementWithdrawal,
	MovementTransfer, MovementAdjustment,
}

const (
	// MaxMovementsPerUser bounds one account's activity log (every account shares one database).
	MaxMovementsPerUser   = 20000
	MaxMovementNoteLength = 200
)

var (
	ErrUnknownMovementKind  = errors.New("unknown movement kind")
	ErrNonPositiveAmount    = errors.New("amount must be greater than 0")
	ErrFeeExceedsAmount     = errors.New("fee can't be larger than the amount")
	ErrOccurredAtOutOfRange = errors.New("occurredAt can't be in the future or before 1970")
	earliestOccurredAt      = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
)

// occurredAtTolerance lets a user ahead of UTC record "today" while it's still yesterday on the server.
const occurredAtTolerance = 24 * time.Hour

// ParseMovementKind reads any kind (filters list the system's kinds too).
func ParseMovementKind(raw string) (MovementKind, error) {
	for _, k := range allMovementKinds {
		if string(k) == raw {
			return k, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownMovementKind, raw)
}

// IsRecordable says whether users record this kind themselves.
func (k MovementKind) IsRecordable() bool {
	for _, r := range RecordableKinds {
		if r == k {
			return true
		}
	}
	return false
}

// HoldingRef is a holding as it was when the movement happened: the activity log still names it after the
// holding is renamed, moved or removed.
type HoldingRef struct {
	Id         HoldingId
	Name       string
	Platform   PlatformName
	AssetClass AssetClass
}

func RefOf(h Holding) HoldingRef {
	return HoldingRef{Id: h.Id, Name: h.Name, Platform: h.Platform, AssetClass: h.AssetClass}
}

// Movement is one recorded change of value. Holdings keep their current value; movements say how it got
// there (and let a mistake be undone).
type Movement struct {
	Id         MovementId
	UserId     UserId
	Kind       MovementKind
	OccurredAt time.Time
	// Amount is what moved, never negative: OPENING and CLOSING may be 0, the rest are > 0. For an
	// ADJUSTMENT it's |NewValue − PreviousValue|.
	Amount Money
	Fee    Money // TRANSFER only: what was lost on the way (the destination gets Amount − Fee)
	// Holding is the holding the movement is about; for a TRANSFER, where the money left.
	Holding   *HoldingRef
	ToHolding *HoldingRef // TRANSFER: where it arrived
	// PreviousValue and NewValue are set when the movement comes from editing a holding's value.
	PreviousValue *Money
	NewValue      *Money
	Note          string
	CreatedAt     time.Time
}

// BalanceChange is what a movement did to one holding's value: a signed amount.
type BalanceChange struct {
	Holding HoldingId
	Delta   decimal.Decimal
}

// Effect is what the movement did to holding values. Undoing it applies the same changes with the signs
// flipped, as deltas: movements recorded since are kept.
func (m Movement) Effect() []BalanceChange {
	amount := m.Amount.Amount()
	switch m.Kind {
	case MovementOpening, MovementGain, MovementDeposit:
		return []BalanceChange{{m.Holding.Id, amount}}
	case MovementClosing, MovementLoss, MovementWithdrawal:
		return []BalanceChange{{m.Holding.Id, amount.Neg()}}
	case MovementTransfer:
		return []BalanceChange{{m.Holding.Id, amount.Neg()}, {m.ToHolding.Id, amount.Sub(m.Fee.Amount())}}
	case MovementAdjustment:
		return []BalanceChange{{m.Holding.Id, m.NewValue.Amount().Sub(m.PreviousValue.Amount())}}
	}
	return nil
}

// Revertible says whether the kind can be undone. Holdings come and go by being added and removed, so
// OPENING and CLOSING can't.
func (k MovementKind) Revertible() bool {
	return k != MovementOpening && k != MovementClosing
}

// PositiveAmount is an amount a user moves: a finite number above 0.
func PositiveAmount(v float64) (Money, error) {
	m, err := NewMoneyFromFloat(v)
	if err != nil {
		return Money{}, err
	}
	if m.IsZero() {
		return Money{}, ErrNonPositiveAmount
	}
	return m, nil
}

// TransferFee is a transfer's fee: 0 or more, and no more than the amount.
func TransferFee(v float64, amount Money) (Money, error) {
	fee, err := NewMoneyFromFloat(v)
	if err != nil {
		return Money{}, err
	}
	if fee.Cmp(amount) > 0 {
		return Money{}, ErrFeeExceedsAmount
	}
	return fee, nil
}

// NormalizeNote trims a movement's note; at most MaxMovementNoteLength characters.
func NormalizeNote(raw string) (string, error) {
	note := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(note); n > MaxMovementNoteLength {
		return "", fmt.Errorf("Note %w (%d > %d)", ErrLabelTooLong, n, MaxMovementNoteLength)
	}
	return note, nil
}

// CheckOccurredAt accepts when a movement happened: not before 1970, not after tomorrow (a day of slack for
// time zones ahead of the server's).
func CheckOccurredAt(at, now time.Time) error {
	if at.Before(earliestOccurredAt) || at.After(now.Add(occurredAtTolerance)) {
		return ErrOccurredAtOutOfRange
	}
	return nil
}

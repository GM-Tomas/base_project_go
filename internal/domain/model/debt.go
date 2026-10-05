package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// DebtKind is what sort of debt it is: it only changes how it's shown.
type DebtKind string

const (
	DebtCreditCard DebtKind = "CREDIT_CARD"
	DebtLoan       DebtKind = "LOAN"
	DebtMortgage   DebtKind = "MORTGAGE"
	DebtPersonal   DebtKind = "PERSONAL" // money a person lent, not a bank
	DebtOther      DebtKind = "OTHER"
)

// DebtKinds are the kinds, in the order the API lists them.
var DebtKinds = []DebtKind{DebtCreditCard, DebtLoan, DebtMortgage, DebtPersonal, DebtOther}

const (
	// Every account shares one database: no single user may grow it without bound.
	MaxDebtsPerUser       = 200
	MaxDebtNameLength     = 120
	MaxDebtLenderLength   = 120
	MaxDebtNotesLength    = 500
	MaxDebtDueDay         = 31
	maxInterestRatePctInt = 200
)

var (
	MaxInterestRatePct        = decimal.NewFromInt(maxInterestRatePctInt)
	ErrUnknownDebtKind        = errors.New("kind must be one of CREDIT_CARD, LOAN, MORTGAGE, PERSONAL, OTHER")
	ErrInterestRateOutOfRange = fmt.Errorf("interestRatePct must be between 0 and %d", maxInterestRatePctInt)
	ErrDueDayOutOfRange       = fmt.Errorf("dueDay must be between 1 and %d", MaxDebtDueDay)
)

// ParseDebtKind reads a kind; none is OTHER.
func ParseDebtKind(raw string) (DebtKind, error) {
	if raw == "" {
		return DebtOther, nil
	}
	for _, k := range DebtKinds {
		if string(k) == raw {
			return k, nil
		}
	}
	return "", fmt.Errorf("%w (got %q)", ErrUnknownDebtKind, raw)
}

// Debt is something a user owes: a card balance, a loan, a mortgage, money a friend lent. Its balance is
// what's left to pay; the rest says how it's paid off, when any of it is known.
type Debt struct {
	Id     DebtId
	UserId UserId
	Name   string
	Lender string // who it's owed to; may be empty
	Kind   DebtKind
	// Balance is what's left to pay.
	Balance Money
	// InterestRatePct is the annual rate (0 to 200), if known.
	InterestRatePct *decimal.Decimal
	// MonthlyPayment is what's paid each month, if there's a set amount.
	MonthlyPayment *Money
	// DueDay is the day of the month a payment is due (1 to 31), if there's one.
	DueDay    *int
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NormalizeDebtName is how a debt's name is stored: a label of at most MaxDebtNameLength.
func NormalizeDebtName(raw string) (string, error) {
	return NormalizeLabel(raw, MaxDebtNameLength, "Debt name")
}

// NormalizeDebtLender is how a lender is stored: a label of at most MaxDebtLenderLength, or nothing.
func NormalizeDebtLender(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	return NormalizeLabel(raw, MaxDebtLenderLength, "Lender")
}

// NormalizeDebtNotes trims a debt's notes; at most MaxDebtNotesLength characters, line breaks kept.
func NormalizeDebtNotes(raw string) (string, error) {
	notes := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(notes); n > MaxDebtNotesLength {
		return "", fmt.Errorf("Notes %w (%d > %d)", ErrLabelTooLong, n, MaxDebtNotesLength)
	}
	return notes, nil
}

// NewInterestRatePct is an annual rate between 0 and 200 percent, kept to 2 decimals.
func NewInterestRatePct(v float64) (decimal.Decimal, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return decimal.Zero, ErrInterestRateOutOfRange
	}
	rate := decimal.NewFromFloat(v).Round(2)
	if rate.IsNegative() || rate.GreaterThan(MaxInterestRatePct) {
		return decimal.Zero, ErrInterestRateOutOfRange
	}
	return rate, nil
}

// NewDueDay is a day of the month, 1 to 31 (a 31 means the month's last day in shorter months).
func NewDueDay(v int) (int, error) {
	if v < 1 || v > MaxDebtDueDay {
		return 0, ErrDueDayOutOfRange
	}
	return v, nil
}

// WithDelta is the debt with its balance changed by delta (positive: it owes more), rounded to cents as
// Money is; one that would drop below zero is ErrNegativeBalance.
func (d Debt) WithDelta(delta decimal.Decimal) (Debt, error) {
	next := d.Balance.Amount().Add(delta)
	if next.IsNegative() {
		return d, ErrNegativeBalance
	}
	d.Balance = MustMoney(next)
	return d, nil
}

// DebtRef is a debt as it was when a movement happened: the activity log still names it after the debt is
// renamed or removed.
type DebtRef struct {
	Id     DebtId
	Name   string
	Lender string
}

func DebtRefOf(d Debt) DebtRef {
	return DebtRef{Id: d.Id, Name: d.Name, Lender: d.Lender}
}

// DebtTotals is what a user owes, all debts together.
type DebtTotals struct {
	Balance        Money
	Count          int
	MonthlyPayment Money // the debts' monthly payments, those that have one
}

// PayoffStatus is whether, and how, a debt gets paid off at its monthly payment.
type PayoffStatus string

const (
	PayoffPaidOff   PayoffStatus = "PAID_OFF"   // nothing left to pay
	PayoffOnTrack   PayoffStatus = "ON_TRACK"   // paid off within MaxPayoffMonths
	PayoffNever     PayoffStatus = "NEVER"      // the payment doesn't cover the interest, or takes too long
	PayoffNoPayment PayoffStatus = "NO_PAYMENT" // no monthly payment to go by
)

// MaxPayoffMonths is the horizon of a payoff estimate: 50 years.
const MaxPayoffMonths = 600

// DebtPayoff is when a debt is paid off at its monthly payment. Months, PayoffMonth and TotalInterest are
// set when it's ON_TRACK.
type DebtPayoff struct {
	Status        PayoffStatus
	Months        *int
	PayoffMonth   *string // "YYYY-MM"
	TotalInterest *Money
}

// BalanceChangeReason is why a debt's balance was edited: it decides what the edit is recorded as.
type BalanceChangeReason string

const (
	ReasonPayment        BalanceChangeReason = "PAYMENT"    // a payment: lower
	ReasonCharge         BalanceChangeReason = "CHARGE"     // new charges: higher
	ReasonInterest       BalanceChangeReason = "INTEREST"   // interest or fees: higher
	ReasonDebtCorrection BalanceChangeReason = "CORRECTION" // an adjustment, either way
)

var (
	ErrUnknownBalanceChangeReason = errors.New("balanceChangeReason must be one of PAYMENT, CHARGE, INTEREST, CORRECTION")
	ErrPaymentRaisesBalance       = errors.New("A payment can only lower the balance")
	ErrChargeLowersBalance        = errors.New("New charges and interest can only raise the balance")
)

// ParseBalanceChangeReason reads a reason; none is a correction (a balance copied from a statement mixes
// charges, interest and payments).
func ParseBalanceChangeReason(raw string) (BalanceChangeReason, error) {
	switch BalanceChangeReason(raw) {
	case "", ReasonDebtCorrection:
		return ReasonDebtCorrection, nil
	case ReasonPayment, ReasonCharge, ReasonInterest:
		return BalanceChangeReason(raw), nil
	}
	return "", fmt.Errorf("%w (got %q)", ErrUnknownBalanceChangeReason, raw)
}

// KindFor is what an edit of a balance from previous to next is recorded as, for this reason: a payment
// lowers it, a charge or interest raises it, a correction does either.
func (r BalanceChangeReason) KindFor(previous, next Money) (MovementKind, error) {
	up := next.Cmp(previous) > 0
	switch r {
	case ReasonPayment:
		if up {
			return "", ErrPaymentRaisesBalance
		}
		return MovementDebtPayment, nil
	case ReasonCharge, ReasonInterest:
		if !up {
			return "", ErrChargeLowersBalance
		}
		if r == ReasonCharge {
			return MovementDebtCharge, nil
		}
		return MovementDebtInterest, nil
	}
	return MovementAdjustment, nil
}

package model

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

var (
	ErrNegativeMoney = errors.New("money must not be negative")
	hundred          = decimal.NewFromInt(100)
	ZeroMoney        = Money{amount: decimal.NewFromInt(0).Truncate(2)}
)

// Money represents a non-negative USD amount, always scale 2.
type Money struct {
	amount decimal.Decimal
}

// NewMoney creates a Money from a decimal.Decimal, rounded half-up to 2 decimals.
func NewMoney(amount decimal.Decimal) (Money, error) {
	if amount.IsNegative() {
		return ZeroMoney, fmt.Errorf("%w: %s", ErrNegativeMoney, amount.String())
	}
	return Money{amount: amount.Round(2)}, nil
}

// MustMoney creates a Money or panics if negative.
func MustMoney(amount decimal.Decimal) Money {
	m, err := NewMoney(amount)
	if err != nil {
		panic(err)
	}
	return m
}

// NewMoneyFromFloat creates a Money from a float64.
func NewMoneyFromFloat(val float64) (Money, error) {
	return NewMoney(decimal.NewFromFloat(val))
}

// MustMoneyFromFloat creates a Money from float64 or panics.
func MustMoneyFromFloat(val float64) Money {
	m, err := NewMoneyFromFloat(val)
	if err != nil {
		panic(err)
	}
	return m
}

// Amount returns the underlying decimal.Decimal.
func (m Money) Amount() decimal.Decimal {
	return m.amount
}

// Float64 returns the amount as float64 rounded to 2 decimals.
func (m Money) Float64() float64 {
	f, _ := m.amount.Round(2).Float64()
	return f
}

// String returns the 2-decimal string representation.
func (m Money) String() string {
	return m.amount.StringFixed(2)
}

// Plus adds two Money values.
func (m Money) Plus(other Money) Money {
	return Money{amount: m.amount.Add(other.amount)}
}

// Minus subtracts other from m, coercing negative results to zero.
func (m Money) Minus(other Money) Money {
	diff := m.amount.Sub(other.amount)
	if diff.IsNegative() {
		return ZeroMoney
	}
	return Money{amount: diff.Round(2)}
}

// Times multiplies Money by a factor, rounded half-up to 2 decimals.
func (m Money) Times(factor decimal.Decimal) Money {
	prod := m.amount.Mul(factor).Round(2)
	if prod.IsNegative() {
		return ZeroMoney
	}
	return Money{amount: prod}
}

// Cmp compares two Money values (-1, 0, 1).
func (m Money) Cmp(other Money) int {
	return m.amount.Cmp(other.amount)
}

// GreaterThanOrEqual returns true if m >= other.
func (m Money) GreaterThanOrEqual(other Money) bool {
	return m.Cmp(other) >= 0
}

// IsZero returns true if amount is zero.
func (m Money) IsZero() bool {
	return m.amount.IsZero()
}

// PercentOf calculates percentage of total (1 decimal). Returns nil if total is 0.
func (m Money) PercentOf(total Money) *decimal.Decimal {
	if total.amount.IsZero() {
		return nil
	}
	pct := m.amount.Mul(hundred).DivRound(total.amount, 1)
	return &pct
}

// GrowthPctFrom calculates percentage growth from baseline (1 decimal). Returns nil if baseline is 0.
func (m Money) GrowthPctFrom(baseline Money) *decimal.Decimal {
	if baseline.amount.IsZero() {
		return nil
	}
	pct := m.amount.Sub(baseline.amount).Mul(hundred).DivRound(baseline.amount, 1)
	return &pct
}

// SumMoney sums a slice of Money.
func SumMoney(values []Money) Money {
	total := decimal.Zero
	for _, v := range values {
		total = total.Add(v.amount)
	}
	return Money{amount: total.Round(2)}
}

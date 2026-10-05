package model

import (
	"github.com/shopspring/decimal"
)

// SignedMoney is a USD amount that can be below zero, always scale 2: a net worth, which is negative when
// debts exceed assets. Everything a user owns or owes is Money (never negative); what they're worth is this.
type SignedMoney struct {
	amount decimal.Decimal
}

var ZeroSignedMoney = SignedMoney{amount: decimal.Zero}

// NewSignedMoney is the amount rounded half-up to 2 decimals.
func NewSignedMoney(amount decimal.Decimal) SignedMoney {
	return SignedMoney{amount: amount.Round(2)}
}

// ParseSignedMoney reads an amount as it's stored ("-1234.50").
func ParseSignedMoney(s string) (SignedMoney, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return SignedMoney{}, err
	}
	return NewSignedMoney(d), nil
}

// NetOf is assets minus debts.
func NetOf(assets, debts Money) SignedMoney {
	return NewSignedMoney(assets.Amount().Sub(debts.Amount()))
}

// Signed is the amount as a SignedMoney.
func (m Money) Signed() SignedMoney {
	return SignedMoney{amount: m.amount}
}

func (s SignedMoney) Amount() decimal.Decimal {
	return s.amount
}

// Float64 returns the amount as float64 rounded to 2 decimals.
func (s SignedMoney) Float64() float64 {
	f, _ := s.amount.Round(2).Float64()
	return f
}

// String returns the 2-decimal representation, with a minus sign when negative.
func (s SignedMoney) String() string {
	return s.amount.StringFixed(2)
}

func (s SignedMoney) Cmp(other SignedMoney) int {
	return s.amount.Cmp(other.amount)
}

// GrowthPctFrom is the percentage change from baseline (1 decimal), or nil when the baseline isn't above
// zero: a change from nothing, or from owing more than owned, has no meaningful percentage.
func (s SignedMoney) GrowthPctFrom(baseline SignedMoney) *decimal.Decimal {
	if !baseline.amount.IsPositive() {
		return nil
	}
	pct := s.amount.Sub(baseline.amount).Mul(hundred).DivRound(baseline.amount, 1)
	return &pct
}

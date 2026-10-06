package model

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/shopspring/decimal"
)

// ErrNegativeBalance is a change that would take a value below zero.
var ErrNegativeBalance = errors.New("value would go below zero")

const maxExpectedReturnPctInt = 100

var (
	MaxExpectedReturnPct        = decimal.NewFromInt(maxExpectedReturnPctInt)
	ErrExpectedReturnOutOfRange = fmt.Errorf("expectedReturnPct must be between -%d and %d",
		maxExpectedReturnPctInt, maxExpectedReturnPctInt)
)

// NewExpectedReturnPct is a yearly return between -100 and 100 percent, kept to 2 decimals.
func NewExpectedReturnPct(v float64) (decimal.Decimal, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return decimal.Zero, ErrExpectedReturnOutOfRange
	}
	pct := decimal.NewFromFloat(v).Round(2)
	if pct.Abs().GreaterThan(MaxExpectedReturnPct) {
		return decimal.Zero, ErrExpectedReturnOutOfRange
	}
	return pct, nil
}

const (
	MaxHoldingNameLength = 120
	// Every account shares one database: no single user may grow it without bound.
	MaxHoldingsPerUser = 1000
)

// Holding represents an asset position owned by a user.
type Holding struct {
	Id         HoldingId
	UserId     UserId
	Name       string
	AssetClass AssetClass
	Platform   PlatformName
	Value      Money
	// ExpectedReturnPct is roughly how much it grows in a year (-100 to 100), if the user said.
	ExpectedReturnPct *decimal.Decimal
	// ClassReturnPct is its class's default return, as the user set it: not stored with the holding, but
	// filled in when it's read for an answer (see EffectiveReturnPct).
	ClassReturnPct *decimal.Decimal
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// EffectiveReturnPct is the yearly return the holding counts with: its own, or else its class's.
func (h Holding) EffectiveReturnPct() *decimal.Decimal {
	if h.ExpectedReturnPct != nil {
		return h.ExpectedReturnPct
	}
	return h.ClassReturnPct
}

// SameReturn is whether two expected returns are the same (none being the same as none).
func SameReturn(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// NormalizeHoldingName is how a holding's name is stored: a label of at most MaxHoldingNameLength.
func NormalizeHoldingName(raw string) (string, error) {
	return NormalizeLabel(raw, MaxHoldingNameLength, "Holding name")
}

// CreateHolding creates a new Holding with a new HoldingId and current timestamps.
func CreateHolding(
	userId UserId,
	name string,
	assetClass AssetClass,
	platform PlatformName,
	value Money,
	now time.Time,
) (Holding, error) {
	normName, err := NormalizeHoldingName(name)
	if err != nil {
		return Holding{}, err
	}
	return Holding{
		Id:         NewHoldingId(),
		UserId:     userId,
		Name:       normName,
		AssetClass: assetClass,
		Platform:   platform,
		Value:      value,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// WithDelta is the holding with its value changed by delta (positive or negative), rounded to cents as
// Money is; one that would drop below zero is ErrNegativeBalance.
func (h Holding) WithDelta(delta decimal.Decimal) (Holding, error) {
	next := h.Value.Amount().Add(delta)
	if next.IsNegative() {
		return h, ErrNegativeBalance
	}
	h.Value = MustMoney(next)
	return h, nil
}

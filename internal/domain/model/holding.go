package model

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// ErrNegativeBalance is a change that would take a value below zero.
var ErrNegativeBalance = errors.New("value would go below zero")

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
	CreatedAt  time.Time
	UpdatedAt  time.Time
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

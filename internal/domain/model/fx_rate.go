package model

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

var (
	ErrNonPositiveFxRate = errors.New("fx rate must be positive")
)

type FxRateSource string

const (
	FxRateSourceFixedConfig FxRateSource = "FIXED_CONFIG"
	FxRateSourceExternalApi FxRateSource = "EXTERNAL_API"
)

// FxRate represents foreign exchange rate info.
type FxRate struct {
	Available bool
	Rate      decimal.Decimal
	AsOf      time.Time
	Source    FxRateSource
}

func KnownFxRate(rate decimal.Decimal, asOf time.Time, source FxRateSource) (FxRate, error) {
	if !rate.IsPositive() {
		return FxRate{}, ErrNonPositiveFxRate
	}
	return FxRate{
		Available: true,
		Rate:      rate,
		AsOf:      asOf,
		Source:    source,
	}, nil
}

func UnavailableFxRate() FxRate {
	return FxRate{
		Available: false,
	}
}

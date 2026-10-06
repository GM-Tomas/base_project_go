package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/shopspring/decimal"
)

// YieldMode is how Estimate picks the yearly growth: the portfolio's expected return, or the user's own.
type YieldMode string

const (
	YieldModePortfolio YieldMode = "PORTFOLIO"
	YieldModeCustom    YieldMode = "CUSTOM"
)

// The largest monthly saving and milestone an estimate takes.
const (
	maxMonthlyContributionUsdInt = 1_000_000_000
	maxMilestoneUsdInt           = 1_000_000_000_000_000
)

var (
	MaxMonthlyContributionUsd = decimal.NewFromInt(maxMonthlyContributionUsdInt)
	MaxMilestoneUsd           = decimal.NewFromInt(maxMilestoneUsdInt)

	ErrUnknownYieldMode       = errors.New("yieldMode must be one of PORTFOLIO, CUSTOM")
	ErrContributionOutOfRange = fmt.Errorf("contributionUsd must be between 0 and %d", maxMonthlyContributionUsdInt)
	ErrCustomYieldOutOfRange  = fmt.Errorf("customYieldPct must be between -%d and %d", maxYieldPctInt, maxYieldPctInt)
	ErrMilestoneOutOfRange    = fmt.Errorf("milestones must be amounts between 0 and %d", maxMilestoneUsdInt)
)

// DefaultMilestones are the projection's milestones until the user picks their own.
func DefaultMilestones() []Money {
	return []Money{MustMoneyFromFloat(150_000), MustMoneyFromFloat(250_000)}
}

// ParseYieldMode reads a yield mode as the API spells it.
func ParseYieldMode(raw string) (YieldMode, error) {
	switch mode := YieldMode(raw); mode {
	case YieldModePortfolio, YieldModeCustom:
		return mode, nil
	}
	return "", fmt.Errorf("%w (got %q)", ErrUnknownYieldMode, raw)
}

// NewMilestone is a milestone amount: 0 to 1e15 dollars, in cents.
func NewMilestone(v float64) (Money, error) {
	m, err := NewMoneyFromFloat(v)
	if err != nil || m.Amount().GreaterThan(MaxMilestoneUsd) {
		return Money{}, ErrMilestoneOutOfRange
	}
	return m, nil
}

// EstimatePreferences is how the user last left Estimate.
type EstimatePreferences struct {
	Contribution          Money
	Years                 int
	YieldMode             YieldMode
	CustomYieldPct        decimal.Decimal
	Milestones            []Money // sorted, at most MaxProjectionMilestones
	InflationPct          decimal.Decimal
	ContributionGrowthPct decimal.Decimal
}

// AutoSnapshot is whether the app saves a checkpoint on its own: never, or once each calendar month (when
// it's opened and the month has none yet).
type AutoSnapshot string

const (
	AutoSnapshotOff     AutoSnapshot = "OFF"
	AutoSnapshotMonthly AutoSnapshot = "MONTHLY"
)

// StartView is the view the app opens on; HistoryPeriod, the period History opens with (a preset: a period
// of the user's own dates isn't one to keep).
type (
	StartView     string
	HistoryPeriod string
)

var (
	StartViews     = []StartView{"dashboard", "platforms", "assets", "debts", "estimate", "history", "settings"}
	HistoryPeriods = []HistoryPeriod{"1M", "3M", "6M", "YTD", "1Y", "3Y", "ALL"}

	ErrUnknownAutoSnapshot  = errors.New("autoSnapshot must be one of OFF, MONTHLY")
	ErrUnknownStartView     = fmt.Errorf("defaultView must be one of %s", joined(StartViews))
	ErrUnknownHistoryPeriod = fmt.Errorf("historyPeriod must be one of %s", joined(HistoryPeriods))
)

func joined[T ~string](values []T) string {
	texts := make([]string, len(values))
	for i, v := range values {
		texts[i] = string(v)
	}
	return strings.Join(texts, ", ")
}

// ParseAutoSnapshot reads whether to save a checkpoint each month, as the API spells it.
func ParseAutoSnapshot(raw string) (AutoSnapshot, error) {
	switch mode := AutoSnapshot(raw); mode {
	case AutoSnapshotOff, AutoSnapshotMonthly:
		return mode, nil
	}
	return "", fmt.Errorf("%w (got %q)", ErrUnknownAutoSnapshot, raw)
}

// ParseStartView reads the view to open on, as the API spells it.
func ParseStartView(raw string) (StartView, error) {
	if view := StartView(raw); slices.Contains(StartViews, view) {
		return view, nil
	}
	return "", fmt.Errorf("%w (got %q)", ErrUnknownStartView, raw)
}

// ParseHistoryPeriod reads the period History opens with, as the API spells it.
func ParseHistoryPeriod(raw string) (HistoryPeriod, error) {
	if period := HistoryPeriod(raw); slices.Contains(HistoryPeriods, period) {
		return period, nil
	}
	return "", fmt.Errorf("%w (got %q)", ErrUnknownHistoryPeriod, raw)
}

// Preferences is what a user set up the way they like it, kept for every device.
type Preferences struct {
	Estimate      EstimatePreferences
	AutoSnapshot  AutoSnapshot
	DefaultView   StartView
	HistoryPeriod HistoryPeriod
}

// DefaultPreferences is what a user who never changed anything gets.
func DefaultPreferences() Preferences {
	return Preferences{
		Estimate: EstimatePreferences{
			Contribution:          MustMoneyFromFloat(900),
			Years:                 12,
			YieldMode:             YieldModePortfolio,
			CustomYieldPct:        decimal.NewFromInt(9),
			Milestones:            DefaultMilestones(),
			InflationPct:          decimal.Zero,
			ContributionGrowthPct: decimal.Zero,
		},
		AutoSnapshot:  AutoSnapshotOff,
		DefaultView:   "dashboard",
		HistoryPeriod: "1Y",
	}
}

// Check is whether the preferences are within the ranges an estimate takes, and the milestones in order.
func (e EstimatePreferences) Check() (EstimatePreferences, error) {
	if e.Contribution.Amount().GreaterThan(MaxMonthlyContributionUsd) {
		return e, ErrContributionOutOfRange
	}
	if e.Years < 1 || e.Years > MaxProjectionYears {
		return e, ErrYearsOutOfRange
	}
	if _, err := ParseYieldMode(string(e.YieldMode)); err != nil {
		return e, err
	}
	if e.CustomYieldPct.Abs().GreaterThan(MaxYieldPct) {
		return e, ErrCustomYieldOutOfRange
	}
	if len(e.Milestones) > MaxProjectionMilestones {
		return e, ErrTooManyMilestones
	}
	for _, m := range e.Milestones {
		if m.Amount().GreaterThan(MaxMilestoneUsd) {
			return e, ErrMilestoneOutOfRange
		}
	}
	params, err := NewProjectionParams(ZeroMoney, e.Contribution, e.CustomYieldPct, e.Years, e.Milestones)
	if err != nil {
		return e, err
	}
	if _, err := params.WithAdjustments(e.InflationPct, e.ContributionGrowthPct); err != nil {
		return e, err
	}
	e.Milestones = params.Milestones
	return e, nil
}

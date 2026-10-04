package model

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"
)

type MilestoneStatus string

const (
	MilestoneStatusAchieved     MilestoneStatus = "ACHIEVED"
	MilestoneStatusReachable    MilestoneStatus = "REACHABLE"
	MilestoneStatusOutOfHorizon MilestoneStatus = "OUT_OF_HORIZON"
)

type Milestone struct {
	Amount         Money
	Status         MilestoneStatus
	MonthsRequired *int
	TargetMonth    *string // Format: "YYYY-MM"
}

type ProjectionPoint struct {
	Year             int
	FutureValue      Money
	TotalContributed Money
	InterestEarned   Money
}

const (
	MaxProjectionYears      = 50
	MaxProjectionMilestones = 5
)

var (
	MaxYieldPct          = decimal.NewFromInt(100)
	ErrYearsOutOfRange   = fmt.Errorf("years must be between 1 and %d", MaxProjectionYears)
	ErrYieldOutOfRange   = errors.New("yieldPct must be between 0 and 100")
	ErrTooManyMilestones = fmt.Errorf("at most %d milestones", MaxProjectionMilestones)
)

// YieldPctFromFloat converts an annual yield percentage, refusing NaN/±Inf (decimal.NewFromFloat panics on
// them) and anything outside 0–100.
func YieldPctFromFloat(v float64) (decimal.Decimal, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return decimal.Zero, ErrYieldOutOfRange
	}
	pct := decimal.NewFromFloat(v)
	if pct.IsNegative() || pct.GreaterThan(MaxYieldPct) {
		return decimal.Zero, ErrYieldOutOfRange
	}
	return pct, nil
}

type ProjectionParams struct {
	Principal           Money
	MonthlyContribution Money
	AnnualYieldPct      decimal.Decimal
	Years               int
	Milestones          []Money
}

func NewProjectionParams(
	principal Money,
	monthlyContribution Money,
	annualYieldPct decimal.Decimal,
	years int,
	milestones []Money,
) (ProjectionParams, error) {
	if years < 1 || years > MaxProjectionYears {
		return ProjectionParams{}, ErrYearsOutOfRange
	}
	if annualYieldPct.IsNegative() || annualYieldPct.GreaterThan(MaxYieldPct) {
		return ProjectionParams{}, ErrYieldOutOfRange
	}
	if len(milestones) > MaxProjectionMilestones {
		return ProjectionParams{}, ErrTooManyMilestones
	}

	sortedMilestones := make([]Money, len(milestones))
	copy(sortedMilestones, milestones)
	sort.Slice(sortedMilestones, func(i, j int) bool {
		return sortedMilestones[i].Cmp(sortedMilestones[j]) < 0
	})

	return ProjectionParams{
		Principal:           principal,
		MonthlyContribution: monthlyContribution,
		AnnualYieldPct:      annualYieldPct,
		Years:               years,
		Milestones:          sortedMilestones,
	}, nil
}

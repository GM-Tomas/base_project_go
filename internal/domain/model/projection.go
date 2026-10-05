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
	FutureValue      Money // the portfolio's
	TotalContributed Money
	InterestEarned   Money
	DebtBalance      Money       // what's still owed then
	NetWorth         SignedMoney // FutureValue − DebtBalance
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
// them). The 0–100 range is NewProjectionParams' to enforce.
func YieldPctFromFloat(v float64) (decimal.Decimal, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return decimal.Zero, ErrYieldOutOfRange
	}
	return decimal.NewFromFloat(v), nil
}

type ProjectionParams struct {
	Principal           Money // the portfolio (assets) the projection starts from
	MonthlyContribution Money
	AnnualYieldPct      decimal.Decimal
	Years               int
	Milestones          []Money
	// DebtBalances is what's owed now and after each month of the horizon (Years*12+1 values), each debt
	// paid off on its own terms; nil without debts. Net worth, and so the milestones, discount it.
	DebtBalances []decimal.Decimal
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

package model

import (
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
	InterestEarned   SignedMoney // FutureValue − TotalContributed: below zero when the yield is
	DebtBalance      Money       // what's still owed then
	NetWorth         SignedMoney // FutureValue − DebtBalance
	// The same in today's dollars: deflated month by month at ProjectionParams.InflationPct (equal to the
	// above without inflation).
	RealFutureValue Money
	RealNetWorth    SignedMoney
}

// YieldSource is where a projection's yearly growth came from.
type YieldSource string

const (
	// YieldFromPortfolio is the portfolio's expected return, weighted by value (0 without one).
	YieldFromPortfolio YieldSource = "PORTFOLIO"
	// YieldCustom is a growth the user picked.
	YieldCustom YieldSource = "CUSTOM"
)

const (
	MaxProjectionYears      = 50
	MaxProjectionMilestones = 5
	maxYieldPctInt          = 100
	maxAdjustmentPctInt     = 50
)

var (
	MaxYieldPct      = decimal.NewFromInt(maxYieldPctInt)
	MaxAdjustmentPct = decimal.NewFromInt(maxAdjustmentPctInt)

	ErrYearsOutOfRange              = fmt.Errorf("years must be between 1 and %d", MaxProjectionYears)
	ErrYieldOutOfRange              = fmt.Errorf("yieldPct must be between -%d and %d", maxYieldPctInt, maxYieldPctInt)
	ErrTooManyMilestones            = fmt.Errorf("at most %d milestones", MaxProjectionMilestones)
	ErrInflationOutOfRange          = fmt.Errorf("inflationPct must be between 0 and %d", maxAdjustmentPctInt)
	ErrContributionGrowthOutOfRange = fmt.Errorf("contributionGrowthPct must be between 0 and %d",
		maxAdjustmentPctInt)
)

// YieldPctFromFloat converts an annual yield percentage, refusing NaN/±Inf (decimal.NewFromFloat panics on
// them). The -100–100 range is NewProjectionParams' to enforce.
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
	// InflationPct (yearly) deflates the series into today's dollars; 0 leaves them as they are.
	InflationPct decimal.Decimal
	// ContributionGrowthPct raises the monthly contribution every 12 months; 0 keeps it as it is.
	ContributionGrowthPct decimal.Decimal
}

// WithAdjustments is the projection with a yearly inflation and a yearly raise of the monthly
// contribution, each between 0 and 50 percent.
func (p ProjectionParams) WithAdjustments(inflationPct, contributionGrowthPct decimal.Decimal) (ProjectionParams, error) {
	if inflationPct.IsNegative() || inflationPct.GreaterThan(MaxAdjustmentPct) {
		return ProjectionParams{}, ErrInflationOutOfRange
	}
	if contributionGrowthPct.IsNegative() || contributionGrowthPct.GreaterThan(MaxAdjustmentPct) {
		return ProjectionParams{}, ErrContributionGrowthOutOfRange
	}
	p.InflationPct, p.ContributionGrowthPct = inflationPct, contributionGrowthPct
	return p, nil
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
	if annualYieldPct.Abs().GreaterThan(MaxYieldPct) {
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

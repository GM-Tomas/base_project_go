package service

import (
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

// debtAt is what's owed after months, per params.DebtBalances (nothing without debts).
func debtAt(params model.ProjectionParams, months int) decimal.Decimal {
	if months < len(params.DebtBalances) {
		return params.DebtBalances[months]
	}
	return decimal.Zero
}

// CalculateSeries generates years + 1 points (year 0 through params.Years). Year 0 equals principal. Each
// point's net worth is the portfolio minus what's still owed then.
func CalculateSeries(params model.ProjectionParams) []model.ProjectionPoint {
	series := make([]model.ProjectionPoint, 0, params.Years+1)

	for year := 0; year <= params.Years; year++ {
		months := year * monthsPerYear
		fv := FutureValue(
			params.Principal,
			params.MonthlyContribution,
			params.AnnualYieldPct,
			months,
		)
		totalContributed := params.Principal.Plus(
			params.MonthlyContribution.Times(decimal.NewFromInt(int64(months))),
		)
		interestEarned := fv.Minus(totalContributed)
		debt := model.MustMoney(debtAt(params, months))

		series = append(series, model.ProjectionPoint{
			Year:             year,
			FutureValue:      fv,
			TotalContributed: totalContributed,
			InterestEarned:   interestEarned,
			DebtBalance:      debt,
			NetWorth:         model.NetOf(fv, debt),
		})
	}

	return series
}

// netWorthMonthsToReach is the first month (0..maxMonths) the net worth — the portfolio minus what's owed —
// reaches threshold, or nil if it doesn't.
func netWorthMonthsToReach(params model.ProjectionParams, threshold model.Money, maxMonths int) *int {
	if len(params.DebtBalances) == 0 {
		return MonthsToReach(threshold, params.Principal, params.MonthlyContribution, params.AnnualYieldPct, maxMonths)
	}
	for month := 0; month <= maxMonths; month++ {
		fv := FutureValue(params.Principal, params.MonthlyContribution, params.AnnualYieldPct, month)
		if fv.Amount().Sub(debtAt(params, month)).GreaterThanOrEqual(threshold.Amount()) {
			m := month
			return &m
		}
	}
	return nil
}

// CalculateMilestones evaluates the status of each milestone within the years*12 horizon, on the net worth
// (the portfolio, without debts).
func CalculateMilestones(params model.ProjectionParams, now time.Time) []model.Milestone {
	maxMonths := params.Years * monthsPerYear
	milestones := make([]model.Milestone, 0, len(params.Milestones))

	for _, amount := range params.Milestones {
		monthsRequired := netWorthMonthsToReach(params, amount, maxMonths)

		if monthsRequired == nil {
			milestones = append(milestones, model.Milestone{
				Amount:         amount,
				Status:         model.MilestoneStatusOutOfHorizon,
				MonthsRequired: nil,
				TargetMonth:    nil,
			})
		} else if *monthsRequired == 0 {
			zero := 0
			milestones = append(milestones, model.Milestone{
				Amount:         amount,
				Status:         model.MilestoneStatusAchieved,
				MonthsRequired: &zero,
				TargetMonth:    nil,
			})
		} else {
			m := *monthsRequired
			targetMonthStr := MonthAfter(now, m)

			milestones = append(milestones, model.Milestone{
				Amount:         amount,
				Status:         model.MilestoneStatusReachable,
				MonthsRequired: &m,
				TargetMonth:    &targetMonthStr,
			})
		}
	}

	return milestones
}

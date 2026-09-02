package service

import (
	"fmt"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

// CalculateSeries generates years + 1 points (year 0 through params.Years). Year 0 equals principal.
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

		series = append(series, model.ProjectionPoint{
			Year:             year,
			FutureValue:      fv,
			TotalContributed: totalContributed,
			InterestEarned:   interestEarned,
		})
	}

	return series
}

// CalculateMilestones evaluates the status of each milestone within the years*12 horizon.
func CalculateMilestones(params model.ProjectionParams, now time.Time) []model.Milestone {
	maxMonths := params.Years * monthsPerYear
	milestones := make([]model.Milestone, 0, len(params.Milestones))

	for _, amount := range params.Milestones {
		monthsRequired := MonthsToReach(
			amount,
			params.Principal,
			params.MonthlyContribution,
			params.AnnualYieldPct,
			maxMonths,
		)

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
			// Compute YearMonth after adding m months (pure year-month arithmetic, matching Java's YearMonth.plusMonths)
			totalMonths := int(now.UTC().Month()) - 1 + m
			targetYear := now.UTC().Year() + totalMonths/12
			targetMonth := (totalMonths % 12) + 1
			targetMonthStr := fmt.Sprintf("%04d-%02d", targetYear, targetMonth)

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

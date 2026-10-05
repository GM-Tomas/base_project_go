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
// point's net worth is the portfolio minus what's still owed then; the real values are both in today's
// dollars.
func CalculateSeries(params model.ProjectionParams) []model.ProjectionPoint {
	states := simulate(params)
	series := make([]model.ProjectionPoint, 0, params.Years+1)

	for year := 0; year <= params.Years; year++ {
		months := year * monthsPerYear
		state := states[months]
		fv := model.MustMoneyFromFloat(state.value)
		totalContributed := model.MustMoneyFromFloat(state.contributed)
		debt := model.MustMoney(debtAt(params, months))
		netWorth := model.NetOf(fv, debt)
		deflator := decimal.NewFromFloat(state.deflator)

		series = append(series, model.ProjectionPoint{
			Year:             year,
			FutureValue:      fv,
			TotalContributed: totalContributed,
			InterestEarned:   model.NetOf(fv, totalContributed),
			DebtBalance:      debt,
			NetWorth:         netWorth,
			RealFutureValue:  model.MustMoney(fv.Amount().Div(deflator)),
			RealNetWorth:     model.NewSignedMoney(netWorth.Amount().Div(deflator)),
		})
	}

	return series
}

// CalculateMilestones evaluates the status of each milestone within the years*12 horizon, on the net worth
// (the portfolio, without debts) month by month, in dollars of each month.
func CalculateMilestones(params model.ProjectionParams, now time.Time) []model.Milestone {
	states := simulate(params)
	netWorthAt := func(month int) decimal.Decimal {
		return model.MustMoneyFromFloat(states[month].value).Amount().Sub(debtAt(params, month))
	}
	milestones := make([]model.Milestone, 0, len(params.Milestones))

	for _, amount := range params.Milestones {
		milestone := model.Milestone{Amount: amount, Status: model.MilestoneStatusOutOfHorizon}
		for month := range states {
			if netWorthAt(month).LessThan(amount.Amount()) {
				continue
			}
			m := month
			milestone.MonthsRequired = &m
			if month == 0 {
				milestone.Status = model.MilestoneStatusAchieved
			} else {
				target := MonthAfter(now, month)
				milestone.Status, milestone.TargetMonth = model.MilestoneStatusReachable, &target
			}
			break
		}
		milestones = append(milestones, milestone)
	}

	return milestones
}

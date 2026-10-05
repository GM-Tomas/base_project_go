package dto

// ProjectionPointResponse is a year of the projection: futureValueUsd is the portfolio's, debtBalanceUsd what's
// still owed then, netWorthUsd the difference.
type ProjectionPointResponse struct {
	Year                int     `json:"year"`
	FutureValueUsd      float64 `json:"futureValueUsd"`
	TotalContributedUsd float64 `json:"totalContributedUsd"`
	InterestEarnedUsd   float64 `json:"interestEarnedUsd"`
	DebtBalanceUsd      float64 `json:"debtBalanceUsd"`
	NetWorthUsd         float64 `json:"netWorthUsd"`
}

type MilestoneResponse struct {
	AmountUsd      float64 `json:"amountUsd"`
	Status         string  `json:"status"`
	MonthsRequired *int    `json:"monthsRequired"`
	TargetMonth    *string `json:"targetMonth"`
}

// ProjectionResponse projects the portfolio (principalUsd: the assets); debtsUsd is what's owed now. The
// milestones are on the net worth.
type ProjectionResponse struct {
	PrincipalUsd           float64                   `json:"principalUsd"`
	DebtsUsd               float64                   `json:"debtsUsd"`
	MonthlyContributionUsd float64                   `json:"monthlyContributionUsd"`
	AnnualYieldPct         float64                   `json:"annualYieldPct"`
	Years                  int                       `json:"years"`
	Series                 []ProjectionPointResponse `json:"series"`
	Milestones             []MilestoneResponse       `json:"milestones"`
}

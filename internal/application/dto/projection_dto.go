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
	// The same two in today's dollars (deflated at inflationPct; as above without inflation).
	RealFutureValueUsd float64 `json:"realFutureValueUsd"`
	RealNetWorthUsd    float64 `json:"realNetWorthUsd"`
}

type MilestoneResponse struct {
	AmountUsd      float64 `json:"amountUsd"`
	Status         string  `json:"status"`
	MonthsRequired *int    `json:"monthsRequired"`
	TargetMonth    *string `json:"targetMonth"`
}

// ProjectionResponse projects the portfolio (principalUsd: the assets); debtsUsd is what's owed now. The
// milestones are on the net worth. annualYieldPct is the growth used: the portfolio's expected return
// (yieldSource PORTFOLIO) or the one asked for (CUSTOM); portfolioYieldPct is the portfolio's either way
// (null with nothing to weigh).
type ProjectionResponse struct {
	PrincipalUsd           float64                   `json:"principalUsd"`
	DebtsUsd               float64                   `json:"debtsUsd"`
	MonthlyContributionUsd float64                   `json:"monthlyContributionUsd"`
	AnnualYieldPct         float64                   `json:"annualYieldPct"`
	YieldSource            string                    `json:"yieldSource"`
	PortfolioYieldPct      *float64                  `json:"portfolioYieldPct"`
	InflationPct           float64                   `json:"inflationPct"`
	ContributionGrowthPct  float64                   `json:"contributionGrowthPct"`
	Years                  int                       `json:"years"`
	Series                 []ProjectionPointResponse `json:"series"`
	Milestones             []MilestoneResponse       `json:"milestones"`
}

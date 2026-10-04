package dto

type ProjectionPointResponse struct {
	Year                int     `json:"year"`
	FutureValueUsd      float64 `json:"futureValueUsd"`
	TotalContributedUsd float64 `json:"totalContributedUsd"`
	InterestEarnedUsd   float64 `json:"interestEarnedUsd"`
}

type MilestoneResponse struct {
	AmountUsd      float64 `json:"amountUsd"`
	Status         string  `json:"status"`
	MonthsRequired *int    `json:"monthsRequired"`
	TargetMonth    *string `json:"targetMonth"`
}

type ProjectionResponse struct {
	PrincipalUsd           float64                   `json:"principalUsd"`
	MonthlyContributionUsd float64                   `json:"monthlyContributionUsd"`
	AnnualYieldPct         float64                   `json:"annualYieldPct"`
	Years                  int                       `json:"years"`
	Series                 []ProjectionPointResponse `json:"series"`
	Milestones             []MilestoneResponse       `json:"milestones"`
}

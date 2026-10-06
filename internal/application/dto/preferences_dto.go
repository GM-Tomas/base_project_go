package dto

// PreferencesDocument is GET and PUT /preferences: the whole of what the user set up. A PUT replaces it;
// what it leaves out takes its default, and what's unknown is ignored.
type PreferencesDocument struct {
	Estimate EstimatePreferencesDocument `json:"estimate"`
	// OFF or MONTHLY: whether the app saves a checkpoint each calendar month on its own.
	AutoSnapshot string `json:"autoSnapshot"`
	// The view the app opens on.
	DefaultView string `json:"defaultView"`
	// The period History opens with (a preset).
	HistoryPeriod string `json:"historyPeriod"`
}

// EstimatePreferencesDocument is Estimate as the user left it.
type EstimatePreferencesDocument struct {
	ContributionUsd       float64   `json:"contributionUsd"`
	Years                 int       `json:"years"`
	YieldMode             string    `json:"yieldMode"`
	CustomYieldPct        float64   `json:"customYieldPct"`
	MilestonesUsd         []float64 `json:"milestonesUsd"`
	InflationPct          float64   `json:"inflationPct"`
	ContributionGrowthPct float64   `json:"contributionGrowthPct"`
}

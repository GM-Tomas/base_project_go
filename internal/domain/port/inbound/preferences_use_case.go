package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// EstimatePreferencesCommand is Estimate as the user left it, as sent: checked by the service.
type EstimatePreferencesCommand struct {
	ContributionUsd       float64
	Years                 int
	YieldMode             string
	CustomYieldPct        float64
	MilestonesUsd         []float64
	InflationPct          float64
	ContributionGrowthPct float64
}

// PreferencesCommand is everything the user set up, as sent: checked by the service.
type PreferencesCommand struct {
	Estimate      EstimatePreferencesCommand
	AutoSnapshot  string
	DefaultView   string
	HistoryPeriod string
}

type PreferencesUseCase interface {
	// GetPreferences is what the user saved, or the defaults.
	GetPreferences(ctx context.Context, userId model.UserId) (model.Preferences, error)
	// ReplacePreferences saves them whole, once checked.
	ReplacePreferences(ctx context.Context, userId model.UserId, cmd PreferencesCommand) (model.Preferences, error)
}

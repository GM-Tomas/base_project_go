package service

import (
	"context"
	"math"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	"github.com/shopspring/decimal"
)

// PreferencesService keeps what each user set up the way they like it (how they left Estimate, the monthly
// checkpoint, the view to open on, History's period), so every device opens the same way.
type PreferencesService struct {
	repo outbound.PreferencesRepository
}

func NewPreferencesService(repo outbound.PreferencesRepository) *PreferencesService {
	return &PreferencesService{repo: repo}
}

var _ inbound.PreferencesUseCase = (*PreferencesService)(nil)

func (s *PreferencesService) GetPreferences(ctx context.Context, userId model.UserId) (model.Preferences, error) {
	saved, err := s.repo.Find(ctx, userId)
	if err != nil {
		return model.Preferences{}, err
	}
	if saved == nil {
		return model.DefaultPreferences(), nil
	}
	return *saved, nil
}

// ReplacePreferences checks everything (the ranges an estimate takes, the choices offered) before saving it
// whole.
func (s *PreferencesService) ReplacePreferences(
	ctx context.Context,
	userId model.UserId,
	cmd inbound.PreferencesCommand,
) (model.Preferences, error) {
	estimate, err := checkEstimate(cmd.Estimate)
	if err != nil {
		return model.Preferences{}, err
	}
	autoSnapshot, err := model.ParseAutoSnapshot(cmd.AutoSnapshot)
	if err != nil {
		return model.Preferences{}, err
	}
	view, err := model.ParseStartView(cmd.DefaultView)
	if err != nil {
		return model.Preferences{}, err
	}
	period, err := model.ParseHistoryPeriod(cmd.HistoryPeriod)
	if err != nil {
		return model.Preferences{}, err
	}
	language, err := model.ParseLanguage(cmd.Language)
	if err != nil {
		return model.Preferences{}, err
	}

	preferences := model.Preferences{Estimate: estimate, AutoSnapshot: autoSnapshot, DefaultView: view, HistoryPeriod: period, Language: language}
	if err := s.repo.Save(ctx, userId, preferences); err != nil {
		return model.Preferences{}, err
	}
	return preferences, nil
}

// checkEstimate reads Estimate as sent: amounts in cents, percentages to two decimals, within their ranges.
func checkEstimate(cmd inbound.EstimatePreferencesCommand) (model.EstimatePreferences, error) {
	contribution, err := model.NewMoneyFromFloat(cmd.ContributionUsd)
	if err != nil {
		return model.EstimatePreferences{}, model.ErrContributionOutOfRange
	}
	mode, err := model.ParseYieldMode(cmd.YieldMode)
	if err != nil {
		return model.EstimatePreferences{}, err
	}
	milestones := make([]model.Money, len(cmd.MilestonesUsd))
	for i, v := range cmd.MilestonesUsd {
		if milestones[i], err = model.NewMilestone(v); err != nil {
			return model.EstimatePreferences{}, err
		}
	}
	pcts := make([]decimal.Decimal, 3)
	for i, sent := range []struct {
		v          float64
		outOfRange error
	}{
		{cmd.CustomYieldPct, model.ErrCustomYieldOutOfRange},
		{cmd.InflationPct, model.ErrInflationOutOfRange},
		{cmd.ContributionGrowthPct, model.ErrContributionGrowthOutOfRange},
	} {
		if math.IsNaN(sent.v) || math.IsInf(sent.v, 0) {
			return model.EstimatePreferences{}, sent.outOfRange
		}
		pcts[i] = decimal.NewFromFloat(sent.v).Round(2)
	}
	return model.EstimatePreferences{
		Contribution:          contribution,
		Years:                 cmd.Years,
		YieldMode:             mode,
		CustomYieldPct:        pcts[0],
		Milestones:            milestones,
		InflationPct:          pcts[1],
		ContributionGrowthPct: pcts[2],
	}.Check()
}

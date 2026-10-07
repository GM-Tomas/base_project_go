package service_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPreferencesRepo struct {
	saved   map[model.UserId]model.Preferences
	findErr error
	saveErr error
}

func (m *mockPreferencesRepo) Find(ctx context.Context, userId model.UserId) (*model.Preferences, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	p, ok := m.saved[userId]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (m *mockPreferencesRepo) Save(ctx context.Context, userId model.UserId, preferences model.Preferences) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved[userId] = preferences
	return nil
}

func preferencesCommand() inbound.PreferencesCommand {
	return inbound.PreferencesCommand{
		Estimate: inbound.EstimatePreferencesCommand{
			ContributionUsd: 1500.555, Years: 20, YieldMode: "CUSTOM", CustomYieldPct: 6.256,
			MilestonesUsd: []float64{500000, 100000}, InflationPct: 3, ContributionGrowthPct: 5,
		},
		AutoSnapshot: "MONTHLY", DefaultView: "history", HistoryPeriod: "YTD", Language: "es",
	}
}

func TestPreferencesService_DefaultsThenWhatWasSaved(t *testing.T) {
	repo := &mockPreferencesRepo{saved: map[model.UserId]model.Preferences{}}
	svc := service.NewPreferencesService(repo)
	ctx := context.Background()
	user, other := model.NewUserId(uuid.New()), model.NewUserId(uuid.New())

	got, err := svc.GetPreferences(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, model.DefaultPreferences(), got)

	saved, err := svc.ReplacePreferences(ctx, user, preferencesCommand())
	require.NoError(t, err)
	assert.Equal(t, model.AutoSnapshotMonthly, saved.AutoSnapshot)
	assert.Equal(t, model.StartView("history"), saved.DefaultView)
	assert.Equal(t, model.HistoryPeriod("YTD"), saved.HistoryPeriod)
	assert.Equal(t, model.Language("es"), saved.Language)
	e := saved.Estimate
	assert.Equal(t, "1500.56", e.Contribution.String())
	assert.Equal(t, 20, e.Years)
	assert.Equal(t, model.YieldModeCustom, e.YieldMode)
	assert.Equal(t, "6.26", e.CustomYieldPct.String())
	assert.Equal(t, []string{"100000.00", "500000.00"}, []string{e.Milestones[0].String(), e.Milestones[1].String()}, "in order")
	assert.Equal(t, "3", e.InflationPct.String())
	assert.Equal(t, "5", e.ContributionGrowthPct.String())

	got, err = svc.GetPreferences(ctx, user)
	require.NoError(t, err)
	assert.Equal(t, saved, got)
	got, err = svc.GetPreferences(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, model.DefaultPreferences(), got, "each user has their own")

	// No milestones at all is fine.
	cmd := preferencesCommand()
	cmd.Estimate.MilestonesUsd = nil
	saved, err = svc.ReplacePreferences(ctx, user, cmd)
	require.NoError(t, err)
	assert.Empty(t, saved.Estimate.Milestones)
}

func TestPreferencesService_ChecksEverythingBeforeSaving(t *testing.T) {
	repo := &mockPreferencesRepo{saved: map[model.UserId]model.Preferences{}}
	svc := service.NewPreferencesService(repo)
	user := model.NewUserId(uuid.New())

	for want, change := range map[error]func(*inbound.PreferencesCommand){
		model.ErrContributionOutOfRange:       func(c *inbound.PreferencesCommand) { c.Estimate.ContributionUsd = -1 },
		model.ErrYearsOutOfRange:              func(c *inbound.PreferencesCommand) { c.Estimate.Years = 0 },
		model.ErrUnknownYieldMode:             func(c *inbound.PreferencesCommand) { c.Estimate.YieldMode = "" },
		model.ErrCustomYieldOutOfRange:        func(c *inbound.PreferencesCommand) { c.Estimate.CustomYieldPct = math.NaN() },
		model.ErrMilestoneOutOfRange:          func(c *inbound.PreferencesCommand) { c.Estimate.MilestonesUsd = []float64{-5} },
		model.ErrTooManyMilestones:            func(c *inbound.PreferencesCommand) { c.Estimate.MilestonesUsd = []float64{1, 2, 3, 4, 5, 6} },
		model.ErrInflationOutOfRange:          func(c *inbound.PreferencesCommand) { c.Estimate.InflationPct = 60 },
		model.ErrContributionGrowthOutOfRange: func(c *inbound.PreferencesCommand) { c.Estimate.ContributionGrowthPct = math.Inf(1) },
		model.ErrUnknownAutoSnapshot:          func(c *inbound.PreferencesCommand) { c.AutoSnapshot = "WEEKLY" },
		model.ErrUnknownStartView:             func(c *inbound.PreferencesCommand) { c.DefaultView = "Dashboard" },
		model.ErrUnknownHistoryPeriod:         func(c *inbound.PreferencesCommand) { c.HistoryPeriod = "CUSTOM" },
		model.ErrUnknownLanguage:              func(c *inbound.PreferencesCommand) { c.Language = "pt" },
	} {
		cmd := preferencesCommand()
		change(&cmd)
		_, err := svc.ReplacePreferences(context.Background(), user, cmd)
		assert.ErrorIs(t, err, want)
	}
	assert.Empty(t, repo.saved)

	repo.saveErr = errors.New("write failed")
	_, err := svc.ReplacePreferences(context.Background(), user, preferencesCommand())
	assert.EqualError(t, err, "write failed")
	repo.findErr = errors.New("read failed")
	_, err = svc.GetPreferences(context.Background(), user)
	assert.EqualError(t, err, "read failed")
}

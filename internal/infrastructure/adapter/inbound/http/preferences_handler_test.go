package http_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreferencesHandler_DefaultsSavesAndReadsBack(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, r, "GET", "/api/v1/preferences", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"estimate": {"contributionUsd": 900, "years": 12, "yieldMode": "PORTFOLIO", "customYieldPct": 9,
		"milestonesUsd": [150000, 250000], "inflationPct": 0, "contributionGrowthPct": 0}}`, rec.Body.String())

	// What's left out takes its default; what's unknown is ignored.
	rec = do(t, r, "PUT", "/api/v1/preferences", json.RawMessage(`{"estimate": {"contributionUsd": 1500, "years": 20,
		"yieldMode": "CUSTOM", "customYieldPct": -1.5, "milestonesUsd": [500000, 100000], "inflationPct": 3.25}, "theme": "dark"}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	saved := decode[dto.PreferencesDocument](t, rec)
	assert.Equal(t, dto.EstimatePreferencesDocument{
		ContributionUsd: 1500, Years: 20, YieldMode: "CUSTOM", CustomYieldPct: -1.5, MilestonesUsd: []float64{100000, 500000},
		InflationPct: 3.25, ContributionGrowthPct: 0,
	}, saved.Estimate)

	rec = do(t, r, "GET", "/api/v1/preferences", nil)
	assert.Equal(t, saved, decode[dto.PreferencesDocument](t, rec))

	// No milestones.
	rec = do(t, r, "PUT", "/api/v1/preferences", json.RawMessage(`{"estimate": {"milestonesUsd": []}}`))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"milestonesUsd":[]`)
}

func TestPreferencesHandler_ReportsEveryProblemAtOnce(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, r, "PUT", "/api/v1/preferences", json.RawMessage(`{"estimate": {"contributionUsd": -1, "years": 51,
		"yieldMode": "MAGIC", "customYieldPct": 101, "milestonesUsd": [1, 2, 3, 4, 5, -6], "inflationPct": 51,
		"contributionGrowthPct": -1}}`))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	prob := decode[middleware.ProblemDetail](t, rec)
	assert.Equal(t, "contributionUsd must be between 0 and 1000000000; years must be between 1 and 50; "+
		"yieldMode must be one of PORTFOLIO, CUSTOM; customYieldPct must be between -100 and 100; at most 5 milestones; "+
		"milestones must be amounts between 0 and 1000000000000000; inflationPct must be between 0 and 50; "+
		"contributionGrowthPct must be between 0 and 50", prob.Detail)
	assert.Equal(t, "estimate.contributionUsd", prob.Errors[0].Field)

	rec = do(t, r, "PUT", "/api/v1/preferences", json.RawMessage(`{"estimate": "soon"}`))
	assert.Equal(t, "Malformed JSON body", decode[middleware.ProblemDetail](t, rec).Detail)
	// Nothing was saved.
	rec = do(t, r, "GET", "/api/v1/preferences", nil)
	assert.Equal(t, 900.0, decode[dto.PreferencesDocument](t, rec).Estimate.ContributionUsd)
}

package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWealthHandler_Summary(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, _, _, _, wealthAgg := setupTestRouter(userId)
	wealthAgg.assets = model.MustMoneyFromFloat(12345.67)

	req := httptest.NewRequest("GET", "/api/v1/wealth/summary", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var summary dto.WealthSummaryResponse
	err := json.NewDecoder(rec.Body).Decode(&summary)
	require.NoError(t, err)

	assert.Equal(t, 12345.67, summary.NetWorth.Usd)
}

func TestWealthHandler_Estimate(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, _, _, _, _ := setupTestRouter(userId)

	req := httptest.NewRequest("GET", "/api/v1/wealth/estimate?contribution=900&yieldPct=9&years=12", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control")) // principal changes with every holding mutation

	var proj dto.ProjectionResponse
	err := json.NewDecoder(rec.Body).Decode(&proj)
	require.NoError(t, err)

	assert.Equal(t, 12, proj.Years)
	assert.Len(t, proj.Series, 13)
	assert.Len(t, proj.Milestones, 2)
}

func TestWealthHandler_Snapshots(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, _, _, _, _ := setupTestRouter(userId)

	// 1. Create Snapshot
	req := httptest.NewRequest("POST", "/api/v1/wealth/snapshots", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "/api/v1/wealth/snapshots/")

	var created dto.SnapshotResponse
	err := json.NewDecoder(rec.Body).Decode(&created)
	require.NoError(t, err)
	assert.Equal(t, 100000.0, created.TotalValueUsd)

	// 2. Duplicate snapshot in same second -> 409
	req = httptest.NewRequest("POST", "/api/v1/wealth/snapshots", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)

	// 3. Get Snapshots
	req = httptest.NewRequest("GET", "/api/v1/wealth/snapshots", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	err = json.NewDecoder(rec.Body).Decode(&list)
	require.NoError(t, err)
	require.Len(t, list, 1)
	// HistoryView renders "—" only for an explicit null; a missing key would crash formatPercentage.
	change, present := list[0]["changePctFromPrevious"]
	assert.True(t, present)
	assert.Nil(t, change)
}

func TestWealthHandler_EstimateValidation(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, _, _, _, _ := setupTestRouter(userId)

	for _, query := range []string{
		"contribution=-10&yieldPct=9&years=12",
		"yieldPct=9&years=12",
		"contribution=abc&yieldPct=9&years=12",
		"contribution=1e10&yieldPct=9&years=12",
		"contribution=900&yieldPct=101&years=12",
		"contribution=900&yieldPct=-100.01&years=12",
		"contribution=900&years=12&milestones=1,2,3,4,5,6",
		"contribution=900&years=12&milestones=1,lots",
		"contribution=900&years=12&milestones=-5",
		"contribution=900&years=12&milestones=1e16",
		"contribution=900&years=12&inflationPct=50.5",
		"contribution=900&years=12&inflationPct=-1",
		"contribution=900&years=12&inflationPct=NaN",
		"contribution=900&years=12&contributionGrowthPct=51",
		"contribution=900&yieldPct=9",
		"contribution=900&yieldPct=9&years=0",
		"contribution=900&yieldPct=9&years=51",
		"contribution=900&yieldPct=9&years=1.5",
		// ParseFloat accepts these, and NaN slips through every < / > check: it used to reach
		// decimal.NewFromFloat and panic (500).
		"contribution=NaN&yieldPct=9&years=12",
		"contribution=900&yieldPct=NaN&years=12",
		"contribution=Inf&yieldPct=9&years=12",
		"contribution=900&yieldPct=-Inf&years=12",
	} {
		req := httptest.NewRequest("GET", "/api/v1/wealth/estimate?"+query, nil)
		req.Header.Set("Authorization", "Bearer token")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code, query)
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json", query)
	}
}

func TestWealthHandler_EstimateReportsEveryProblemAtOnce(t *testing.T) {
	router, _, _, _, _ := setupTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, router, "GET", "/api/v1/wealth/estimate?yieldPct=200&years=60&milestones=x&inflationPct=99&contributionGrowthPct=-1", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var problem middleware.ProblemDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&problem))
	assert.Equal(t, "contribution is required; yieldPct must be between -100 and 100; years must be between 1 and 50; "+
		"milestones must be up to 5 comma-separated amounts between 0 and 1000000000000000; inflationPct must be between 0 and 50; "+
		"contributionGrowthPct must be between 0 and 50", problem.Detail)
}

func TestWealthHandler_EstimateAtThePortfoliosReturn(t *testing.T) {
	r := newTestRouter(model.NewUserId(uuid.New()))
	eight := decimal.NewFromInt(8)
	r.wealthAgg.returns = []model.HoldingReturn{{Value: model.MustMoneyFromFloat(100000), Pct: &eight}}

	estimate := func(query string) dto.ProjectionResponse {
		t.Helper()
		rec := do(t, r, "GET", "/api/v1/wealth/estimate?"+query, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var res dto.ProjectionResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&res))
		return res
	}

	portfolio := estimate("contribution=0&years=1")
	assert.Equal(t, "PORTFOLIO", portfolio.YieldSource)
	assert.Equal(t, 8.0, portfolio.AnnualYieldPct)
	assert.Equal(t, 8.0, *portfolio.PortfolioYieldPct)
	assert.Equal(t, []float64{150000, 250000}, []float64{portfolio.Milestones[0].AmountUsd, portfolio.Milestones[1].AmountUsd})
	assert.Equal(t, portfolio.Series[1].FutureValueUsd, portfolio.Series[1].RealFutureValueUsd)

	custom := estimate("contribution=100&years=2&yieldPct=-3.5&milestones=900000,%20120000&inflationPct=4&contributionGrowthPct=10")
	assert.Equal(t, "CUSTOM", custom.YieldSource)
	assert.Equal(t, -3.5, custom.AnnualYieldPct)
	assert.Equal(t, 8.0, *custom.PortfolioYieldPct)
	assert.Equal(t, 4.0, custom.InflationPct)
	assert.Equal(t, 10.0, custom.ContributionGrowthPct)
	assert.Equal(t, []float64{120000, 900000}, []float64{custom.Milestones[0].AmountUsd, custom.Milestones[1].AmountUsd}, "in order")
	assert.Less(t, custom.Series[2].RealNetWorthUsd, custom.Series[2].NetWorthUsd)
	// 1,200 the first year, 1,320 the second.
	assert.Equal(t, 100000.0+2520, custom.Series[2].TotalContributedUsd)
	assert.Negative(t, custom.Series[2].InterestEarnedUsd)

	assert.Empty(t, estimate("contribution=0&years=1&milestones=").Milestones, "none asked for")
}

func TestWealthHandler_DeleteSnapshot(t *testing.T) {
	router, _, _, _, _ := setupTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, router, "POST", "/api/v1/wealth/snapshots", nil)
	require.Equal(t, http.StatusCreated, rec.Code)
	var snap dto.SnapshotResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&snap))

	rec = do(t, router, "DELETE", "/api/v1/wealth/snapshots/"+snap.Id, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = do(t, router, "GET", "/api/v1/wealth/snapshots", nil)
	assert.JSONEq(t, "[]", rec.Body.String())

	for _, id := range []string{snap.Id, "not-a-uuid"} {
		rec = do(t, router, "DELETE", "/api/v1/wealth/snapshots/"+id, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, id)
		assert.Contains(t, rec.Body.String(), "Snapshot "+id+" not found")
	}
}

package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWealthHandler_Summary(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, _, _, _, wealthAgg := setupTestRouter(userId)
	wealthAgg.netWorth = model.MustMoneyFromFloat(12345.67)

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
	assert.Empty(t, rec.Header().Get("Cache-Control")) // principal changes with every holding mutation

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

	req := httptest.NewRequest("GET", "/api/v1/wealth/estimate?contribution=-10&yieldPct=9&years=12", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
}

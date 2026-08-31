package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	appHttp "github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dummyJWTValidator struct {
	userId model.UserId
}

func (v *dummyJWTValidator) ValidateToken(ctx context.Context, tokenStr string) (model.UserId, error) {
	return v.userId, nil
}

func setupTestRouter(userId model.UserId) (http.Handler, *mockHoldingRepo, *mockPlatformRepo, *mockSnapshotRepo, *mockWealthAggregationPort) {
	holdingRepo := newMockHoldingRepo()
	platformRepo := newMockPlatformRepo()
	snapshotRepo := newMockSnapshotRepo()
	wealthAgg := &mockWealthAggregationPort{
		netWorth: model.MustMoneyFromFloat(100000.0),
	}

	clock := func() time.Time { return time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC) }

	holdingSvc := service.NewHoldingService(holdingRepo, platformRepo, clock)
	platformSvc := service.NewPlatformService(platformRepo, clock)
	assetClassSvc := service.NewAssetClassService(holdingRepo, nil)
	snapshotSvc := service.NewSnapshotService(snapshotRepo, wealthAgg, clock)
	projSvc := service.NewProjectionService(wealthAgg, clock)
	wealthSvc := service.NewWealthQueryService(wealthAgg, snapshotRepo, clock, nil, 1050.0)

	router := appHttp.NewRouter(appHttp.RouterParams{
		AllowedOrigins:    []string{"*"},
		JWTValidator:      &dummyJWTValidator{userId: userId},
		HealthHandler:     appHttp.NewHealthHandler(),
		HoldingHandler:    appHttp.NewHoldingHandler(holdingSvc),
		PlatformHandler:   appHttp.NewPlatformHandler(platformSvc),
		AssetClassHandler: appHttp.NewAssetClassHandler(assetClassSvc),
		WealthHandler:     appHttp.NewWealthHandler(wealthSvc, snapshotSvc, projSvc),
	})

	return router, holdingRepo, platformRepo, snapshotRepo, wealthAgg
}

func TestHoldingHandler_Endpoints(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, holdingRepo, _, _, _ := setupTestRouter(userId)

	// 1. Create holding
	body, _ := json.Marshal(dto.CreateHoldingRequest{
		Name:       "Solana",
		AssetClass: "Crypto",
		Platform:   "Binance",
		ValueUsd:   2250.0,
	})

	req := httptest.NewRequest("POST", "/api/v1/holdings", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "/api/v1/holdings/")

	var created dto.HoldingResponse
	err := json.NewDecoder(rec.Body).Decode(&created)
	require.NoError(t, err)
	assert.Equal(t, "Solana", created.Name)
	assert.Equal(t, 2250.0, created.ValueUsd)

	// 2. Get All Holdings
	req = httptest.NewRequest("GET", "/api/v1/holdings", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var list []dto.HoldingResponse
	err = json.NewDecoder(rec.Body).Decode(&list)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// 3. Get Holding By ID
	req = httptest.NewRequest("GET", "/api/v1/holdings/"+created.Id, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var fetched dto.HoldingResponse
	err = json.NewDecoder(rec.Body).Decode(&fetched)
	require.NoError(t, err)
	assert.Equal(t, created.Id, fetched.Id)

	// 4. Patch Holding
	newVal := 3000.0
	patchBody, _ := json.Marshal(dto.UpdateHoldingRequest{ValueUsd: &newVal})
	req = httptest.NewRequest("PATCH", "/api/v1/holdings/"+created.Id, bytes.NewReader(patchBody))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var patched dto.HoldingResponse
	err = json.NewDecoder(rec.Body).Decode(&patched)
	require.NoError(t, err)
	assert.Equal(t, 3000.0, patched.ValueUsd)

	// 5. Delete Holding
	req = httptest.NewRequest("DELETE", "/api/v1/holdings/"+created.Id, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)

	// 6. Delete again -> 404
	req = httptest.NewRequest("DELETE", "/api/v1/holdings/"+created.Id, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
	_ = holdingRepo
}

func TestHoldingHandler_ValidationErrors(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, _, _, _, _ := setupTestRouter(userId)

	body, _ := json.Marshal(dto.CreateHoldingRequest{
		Name:       "",
		AssetClass: "",
		Platform:   "",
		ValueUsd:   -10,
	})

	req := httptest.NewRequest("POST", "/api/v1/holdings", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")

	var prob middleware.ProblemDetail
	err := json.NewDecoder(rec.Body).Decode(&prob)
	require.NoError(t, err)
	assert.Len(t, prob.Errors, 4)
}

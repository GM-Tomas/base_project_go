package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	appHttp "github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
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
	platformRepo := newMockPlatformRepo(holdingRepo)
	snapshotRepo := newMockSnapshotRepo()
	wealthAgg := &mockWealthAggregationPort{
		netWorth: model.MustMoneyFromFloat(100000.0),
	}

	clock := func() time.Time { return time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC) }

	holdingSvc := service.NewHoldingService(holdingRepo, platformRepo, clock)
	platformSvc := service.NewPlatformService(platformRepo)
	assetClassSvc := service.NewAssetClassService(holdingRepo, nil)
	snapshotSvc := service.NewSnapshotService(snapshotRepo, wealthAgg, clock)
	projSvc := service.NewProjectionService(wealthAgg, clock)
	wealthSvc := service.NewWealthQueryService(wealthAgg, snapshotRepo, clock, nil)

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

func do(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestHoldingHandler_Endpoints(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, _, _, _, _ := setupTestRouter(userId)

	// 1. Create holding (what AddAssetModal sends)
	rec := do(t, router, "POST", "/api/v1/holdings", dto.CreateHoldingRequest{
		Name: "Solana", AssetClass: "Crypto", Platform: "Binance", ValueUsd: 2250.0,
	})
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "/api/v1/holdings/")

	var created dto.HoldingResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))
	assert.Equal(t, "Solana", created.Name)
	assert.Equal(t, 2250.0, created.ValueUsd)

	// 2. List holdings; the new platform shows up in /platforms
	rec = do(t, router, "GET", "/api/v1/holdings", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	var list []dto.HoldingResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))
	assert.Len(t, list, 1)

	rec = do(t, router, "GET", "/api/v1/platforms", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	var platforms []dto.PlatformResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&platforms))
	require.Len(t, platforms, 1)
	assert.Equal(t, "Binance", platforms[0].Name)

	// 3. Delete holding -> its now-empty platform is gone too
	rec = do(t, router, "DELETE", "/api/v1/holdings/"+created.Id, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = do(t, router, "GET", "/api/v1/platforms", nil)
	assert.JSONEq(t, "[]", rec.Body.String())

	// 4. Delete again -> 404
	rec = do(t, router, "DELETE", "/api/v1/holdings/"+created.Id, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
}

func TestHoldingHandler_EmptyListIsJSONArray(t *testing.T) {
	router, _, _, _, _ := setupTestRouter(model.NewUserId(uuid.New()))

	for _, path := range []string{"/api/v1/holdings", "/api/v1/platforms", "/api/v1/wealth/snapshots"} {
		rec := do(t, router, "GET", path, nil)
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.JSONEq(t, "[]", rec.Body.String(), path) // the frontend maps over these
	}
}

func TestHoldingHandler_ValidationErrors(t *testing.T) {
	router, _, _, _, _ := setupTestRouter(model.NewUserId(uuid.New()))

	rec := do(t, router, "POST", "/api/v1/holdings", dto.CreateHoldingRequest{ValueUsd: -10})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")

	var prob middleware.ProblemDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&prob))
	assert.Len(t, prob.Errors, 4)
	assert.Contains(t, prob.Detail, "Name is required") // the frontend shows detail

	rec = do(t, router, "POST", "/api/v1/holdings", dto.CreateHoldingRequest{Name: "x", AssetClass: "Cash", Platform: "Bank", ValueUsd: 1e30})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRouter_CapsRequestBodyAndSetsSecurityHeaders(t *testing.T) {
	router, holdingRepo, _, _, _ := setupTestRouter(model.NewUserId(uuid.New()))

	// 1 MB name: the body is cut off at the cap before validation ever sees it.
	rec := do(t, router, "POST", "/api/v1/holdings", dto.CreateHoldingRequest{Name: string(bytes.Repeat([]byte("a"), 1<<20)), AssetClass: "Cash", Platform: "Bank"})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var prob middleware.ProblemDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&prob))
	assert.Equal(t, "Malformed JSON body", prob.Detail)
	assert.Empty(t, holdingRepo.holdings)

	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
}

func TestRouter_OnlyExposesWhatTheFrontendUses(t *testing.T) {
	router, _, _, _, _ := setupTestRouter(model.NewUserId(uuid.New()))
	id := uuid.New().String()

	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/holdings/" + id},
		{"PATCH", "/api/v1/holdings/" + id},
		{"POST", "/api/v1/platforms"},
		{"PATCH", "/api/v1/platforms/Binance"},
		{"DELETE", "/api/v1/platforms/Binance"},
	} {
		rec := do(t, router, tc.method, tc.path, nil)
		assert.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code, tc.method+" "+tc.path)
	}
}

func TestRouter_AccessLogIPCannotBeSpoofedWithTrueClientIP(t *testing.T) {
	var buf bytes.Buffer
	defaultLogger := chimiddleware.DefaultLogger
	chimiddleware.DefaultLogger = chimiddleware.RequestLogger(&chimiddleware.DefaultLogFormatter{Logger: log.New(&buf, "", 0), NoColor: true})
	defer func() { chimiddleware.DefaultLogger = defaultLogger }()
	router, _, _, _, _ := setupTestRouter(model.NewUserId(uuid.New()))

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	req.Header.Set("True-Client-IP", "6.6.6.6")
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	router.ServeHTTP(httptest.NewRecorder(), req)

	assert.Contains(t, buf.String(), "from 203.0.113.7")
	assert.NotContains(t, buf.String(), "6.6.6.6")
}

func TestHoldingHandler_PerUserCapIsAConflict(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, holdingRepo, _, _, _ := setupTestRouter(userId)
	for i := 0; i < model.MaxHoldingsPerUser; i++ {
		h := model.Holding{Id: model.NewHoldingId(), UserId: userId, Platform: model.MustPlatformName("Bank")}
		holdingRepo.holdings[h.Id.String()] = h
	}

	rec := do(t, router, "POST", "/api/v1/holdings", dto.CreateHoldingRequest{Name: "x", AssetClass: "Cash", Platform: "Bank", ValueUsd: 1})

	assert.Equal(t, http.StatusConflict, rec.Code)
	var prob middleware.ProblemDetail
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&prob))
	assert.Equal(t, middleware.ProblemBaseURI+"/limit-exceeded", prob.Type)
	assert.Contains(t, prob.Detail, "1000 holdings")
}

package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appHttp "github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthHandler_CheckHealth(t *testing.T) {
	handler := appHttp.NewHealthHandler()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	handler.CheckHealth(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var res appHttp.HealthStatusResponse
	err := json.NewDecoder(rec.Body).Decode(&res)
	require.NoError(t, err)

	assert.Equal(t, "UP", res.Status)
	assert.Equal(t, "base-wealth-backend", res.Service)
	assert.Equal(t, "1.0.0", res.Version)
}

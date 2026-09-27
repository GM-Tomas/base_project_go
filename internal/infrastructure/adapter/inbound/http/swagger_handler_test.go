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

func TestSwaggerHandler_ServeOpenAPIJSON(t *testing.T) {
	handler := appHttp.NewSwaggerHandler()

	req := httptest.NewRequest("GET", "/api/v1/openapi.json", nil)
	rec := httptest.NewRecorder()

	handler.ServeOpenAPIJSON(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var data map[string]interface{}
	err := json.NewDecoder(rec.Body).Decode(&data)
	require.NoError(t, err)
	assert.Equal(t, "3.1.0", data["openapi"])
	assert.NotNil(t, data["paths"])
}

func TestSwaggerHandler_ServeSwaggerUI(t *testing.T) {
	handler := appHttp.NewSwaggerHandler()

	req := httptest.NewRequest("GET", "/swagger", nil)
	rec := httptest.NewRecorder()

	handler.ServeSwaggerUI(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "SwaggerUIBundle")
	assert.Contains(t, rec.Body.String(), "/api/v1/openapi.json")
}

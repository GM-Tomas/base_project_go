package http_test

import (
	"bytes"
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

func TestPlatformHandler_Endpoints(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, _, platformRepo, _, _ := setupTestRouter(userId)

	// 1. Create Platform
	body, _ := json.Marshal(dto.CreatePlatformRequest{
		Name: "Balanz",
		Type: "Broker",
	})
	req := httptest.NewRequest("POST", "/api/v1/platforms", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var created dto.PlatformResponse
	err := json.NewDecoder(rec.Body).Decode(&created)
	require.NoError(t, err)
	assert.Equal(t, "Balanz", created.Name)
	assert.Equal(t, "Broker", created.Type)

	// 2. Duplicate Platform -> 409
	req = httptest.NewRequest("POST", "/api/v1/platforms", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)

	// 3. Get Platforms
	req = httptest.NewRequest("GET", "/api/v1/platforms", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var list []dto.PlatformResponse
	err = json.NewDecoder(rec.Body).Decode(&list)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// 4. Patch Platform
	newType := "Securities Broker"
	patchBody, _ := json.Marshal(dto.PatchPlatformRequest{Type: &newType})
	req = httptest.NewRequest("PATCH", "/api/v1/platforms/Balanz", bytes.NewReader(patchBody))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var patched dto.PlatformResponse
	err = json.NewDecoder(rec.Body).Decode(&patched)
	require.NoError(t, err)
	assert.Equal(t, "Securities Broker", patched.Type)

	// 5. Delete Platform with holdings -> 409
	platformRepo.holdings["Balanz"] = 3
	req = httptest.NewRequest("DELETE", "/api/v1/platforms/Balanz", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)

	// 6. Delete Platform without holdings -> 204
	platformRepo.holdings["Balanz"] = 0
	req = httptest.NewRequest("DELETE", "/api/v1/platforms/Balanz", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

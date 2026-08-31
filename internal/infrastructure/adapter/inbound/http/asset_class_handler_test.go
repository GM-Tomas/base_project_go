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

func TestAssetClassHandler_GetAvailableAssetClasses(t *testing.T) {
	userId := model.NewUserId(uuid.New())
	router, holdingRepo, _, _, _ := setupTestRouter(userId)

	holdingRepo.assetClasses = []model.AssetClass{
		model.MustAssetClass("Crypto"),
	}

	req := httptest.NewRequest("GET", "/api/v1/asset-classes", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var res dto.AvailableAssetClassesResponse
	err := json.NewDecoder(rec.Body).Decode(&res)
	require.NoError(t, err)

	assert.Contains(t, res.Defaults, "Cash")
	assert.Contains(t, res.InUse, "Crypto")
	assert.Contains(t, res.All, "Cash")
}

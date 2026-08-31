package service_test

import (
	"context"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetClassService_GetAvailableAssetClasses(t *testing.T) {
	holdingRepo := newMockHoldingRepo()
	holdingRepo.assetClasses = []model.AssetClass{
		model.MustAssetClass("Crypto"),
		model.MustAssetClass("Real Estate"),
	}

	svc := service.NewAssetClassService(holdingRepo, nil)
	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	res, err := svc.GetAvailableAssetClasses(ctx, userId)
	require.NoError(t, err)

	assert.Equal(t, service.DefaultAssetClasses, res.Defaults)
	assert.Equal(t, []string{"Crypto", "Real Estate"}, res.InUse)
	assert.Contains(t, res.All, "Cash")
	assert.Contains(t, res.All, "Crypto")
	assert.Contains(t, res.All, "Real Estate")
	assert.Len(t, res.All, 6) // 5 defaults + 1 new (Real Estate)
}

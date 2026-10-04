package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformService_GetAllPlatforms(t *testing.T) {
	platformRepo := newMockPlatformRepo(newMockHoldingRepo())
	svc := service.NewPlatformService(platformRepo)

	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	_, err := platformRepo.EnsureExists(ctx, userId, model.MustPlatformName("Binance"), time.Now())
	require.NoError(t, err)
	_, err = platformRepo.EnsureExists(ctx, model.NewUserId(uuid.New()), model.MustPlatformName("Nexo"), time.Now())
	require.NoError(t, err)

	platforms, err := svc.GetAllPlatforms(ctx, userId)
	require.NoError(t, err)
	require.Len(t, platforms, 1) // only the caller's
	assert.Equal(t, "Binance", platforms[0].Name.Value())
}

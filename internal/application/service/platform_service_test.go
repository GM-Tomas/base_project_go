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
	holdingRepo := newMockHoldingRepo()
	svc := service.NewPlatformService(newMockPlatformRepo(holdingRepo))
	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	for _, h := range []model.Holding{
		{Id: model.NewHoldingId(), UserId: userId, Platform: model.MustPlatformName("Binance"), CreatedAt: time.Now()},
		{Id: model.NewHoldingId(), UserId: model.NewUserId(uuid.New()), Platform: model.MustPlatformName("Nexo"), CreatedAt: time.Now()},
	} {
		holdingRepo.holdings[h.Id.String()] = h
	}

	platforms, err := svc.GetAllPlatforms(ctx, userId)
	require.NoError(t, err)
	require.Len(t, platforms, 1) // only the caller's
	assert.Equal(t, "Binance", platforms[0].Name.Value())
}

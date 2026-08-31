package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/service"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformService_CRUD(t *testing.T) {
	platformRepo := newMockPlatformRepo()
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewPlatformService(platformRepo, fixedClock(now))

	userId := model.NewUserId(uuid.New())
	ctx := context.Background()

	// 1. Create platform
	created, err := svc.CreatePlatform(ctx, inbound.CreatePlatformCommand{
		UserId: userId,
		Name:   "Binance",
		Type:   "Exchange",
	})
	require.NoError(t, err)
	assert.Equal(t, "Binance", created.Name.Value())
	assert.Equal(t, "Exchange", created.Type.Value())

	// 2. Duplicate platform (case-insensitive) -> 409 error
	_, err = svc.CreatePlatform(ctx, inbound.CreatePlatformCommand{
		UserId: userId,
		Name:   "binance",
		Type:   "Exchange",
	})
	assert.Error(t, err)

	// 3. Patch platform
	newType := "Crypto Exchange"
	patched, err := svc.PatchPlatform(ctx, inbound.PatchPlatformCommand{
		UserId:  userId,
		Name:    "Binance",
		NewType: &newType,
	})
	require.NoError(t, err)
	assert.Equal(t, "Crypto Exchange", patched.Type.Value())

	// 4. Delete platform with holdings -> error (409)
	platformRepo.holdings["Binance"] = 2
	err = svc.DeletePlatform(ctx, userId, "Binance")
	assert.Error(t, err)

	// 5. Delete platform without holdings -> ok
	platformRepo.holdings["Binance"] = 0
	err = svc.DeletePlatform(ctx, userId, "Binance")
	require.NoError(t, err)
}

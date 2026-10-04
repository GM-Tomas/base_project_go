package service

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
)

type PlatformService struct {
	platformRepo outbound.PlatformRepository
}

func NewPlatformService(platformRepo outbound.PlatformRepository) *PlatformService {
	return &PlatformService{platformRepo: platformRepo}
}

var _ inbound.PlatformUseCase = (*PlatformService)(nil)

func (s *PlatformService) GetAllPlatforms(ctx context.Context, userId model.UserId) ([]model.Platform, error) {
	return s.platformRepo.FindAll(ctx, userId)
}

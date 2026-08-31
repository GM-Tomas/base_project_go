package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
)

type PlatformService struct {
	platformRepo outbound.PlatformRepository
	clock        Clock
}

func NewPlatformService(platformRepo outbound.PlatformRepository, clock Clock) *PlatformService {
	if clock == nil {
		clock = RealClock
	}
	return &PlatformService{
		platformRepo: platformRepo,
		clock:        clock,
	}
}

var _ inbound.PlatformUseCase = (*PlatformService)(nil)

func (s *PlatformService) GetAllPlatforms(ctx context.Context, userId model.UserId) ([]model.Platform, error) {
	return s.platformRepo.FindAll(ctx, userId)
}

func (s *PlatformService) CreatePlatform(ctx context.Context, command inbound.CreatePlatformCommand) (model.Platform, error) {
	name, err := model.NewPlatformName(command.Name)
	if err != nil {
		return model.Platform{}, err
	}

	existing, err := s.platformRepo.FindByName(ctx, command.UserId, name)
	if err != nil {
		return model.Platform{}, err
	}
	if existing != nil {
		return model.Platform{}, appErrors.NewDuplicateResourceError(fmt.Sprintf("Platform '%s' already exists", command.Name))
	}

	pType, err := model.NewPlatformType(command.Type)
	if err != nil {
		return model.Platform{}, err
	}

	platform := model.NewPlatform(command.UserId, name, pType, s.clock())
	return s.platformRepo.Save(ctx, platform)
}

func (s *PlatformService) PatchPlatform(ctx context.Context, command inbound.PatchPlatformCommand) (model.Platform, error) {
	currentName, err := model.NewPlatformName(command.Name)
	if err != nil {
		return model.Platform{}, err
	}

	var newName *model.PlatformName
	if command.NewName != nil {
		pn, err := model.NewPlatformName(*command.NewName)
		if err != nil {
			return model.Platform{}, err
		}
		if !strings.EqualFold(pn.Value(), command.Name) {
			existing, err := s.platformRepo.FindByName(ctx, command.UserId, pn)
			if err != nil {
				return model.Platform{}, err
			}
			if existing != nil {
				return model.Platform{}, appErrors.NewDuplicateResourceError(fmt.Sprintf("Platform '%s' already exists", *command.NewName))
			}
		}
		newName = &pn
	}

	var newType *model.PlatformType
	if command.NewType != nil {
		pt, err := model.NewPlatformType(*command.NewType)
		if err != nil {
			return model.Platform{}, err
		}
		newType = &pt
	}

	updated, err := s.platformRepo.Update(ctx, command.UserId, currentName, newName, newType)
	if err != nil {
		return model.Platform{}, err
	}
	if updated == nil {
		return model.Platform{}, appErrors.NewResourceNotFoundError(fmt.Sprintf("No se encontró la plataforma: %s", command.Name))
	}

	return *updated, nil
}

func (s *PlatformService) DeletePlatform(ctx context.Context, userId model.UserId, name string) error {
	platformName, err := model.NewPlatformName(name)
	if err != nil {
		return err
	}

	count, err := s.platformRepo.CountHoldings(ctx, userId, platformName)
	if err != nil {
		return err
	}
	if count > 0 {
		return appErrors.NewResourceInUseError(fmt.Sprintf("Platform '%s' still has %d holdings", name, count))
	}

	deleted, err := s.platformRepo.DeleteByName(ctx, userId, platformName)
	if err != nil {
		return err
	}
	if !deleted {
		return appErrors.NewResourceNotFoundError(fmt.Sprintf("No se encontró la plataforma: %s", name))
	}

	return nil
}

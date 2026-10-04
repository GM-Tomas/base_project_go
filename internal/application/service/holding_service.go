package service

import (
	"context"
	"fmt"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
)

type Clock func() time.Time

func RealClock() time.Time {
	return time.Now().UTC()
}

type HoldingService struct {
	holdingRepo  outbound.HoldingRepository
	platformRepo outbound.PlatformRepository
	clock        Clock
}

func NewHoldingService(
	holdingRepo outbound.HoldingRepository,
	platformRepo outbound.PlatformRepository,
	clock Clock,
) *HoldingService {
	if clock == nil {
		clock = RealClock
	}
	return &HoldingService{
		holdingRepo:  holdingRepo,
		platformRepo: platformRepo,
		clock:        clock,
	}
}

var _ inbound.HoldingUseCase = (*HoldingService)(nil)

func (s *HoldingService) GetAllHoldings(
	ctx context.Context,
	userId model.UserId,
) ([]model.Holding, error) {
	return s.holdingRepo.FindAll(ctx, userId)
}

func (s *HoldingService) CreateHolding(
	ctx context.Context,
	command inbound.CreateHoldingCommand,
) (model.Holding, error) {
	now := s.clock()

	// Validate everything before touching storage: a rejected request (e.g. a name that's too long)
	// used to leave its brand-new platform behind, showing up as an empty $0 account.
	platform, err := model.NewPlatformName(command.Platform)
	if err != nil {
		return model.Holding{}, err
	}

	ac, err := model.NewAssetClass(command.AssetClass)
	if err != nil {
		return model.Holding{}, err
	}

	moneyVal, err := model.NewMoneyFromFloat(command.ValueUsd)
	if err != nil {
		return model.Holding{}, err
	}

	holding, err := model.CreateHolding(
		command.UserId,
		command.Name,
		ac,
		platform,
		moneyVal,
		now,
	)
	if err != nil {
		return model.Holding{}, err
	}

	count, err := s.holdingRepo.Count(ctx, command.UserId)
	if err != nil {
		return model.Holding{}, err
	}
	if count >= model.MaxHoldingsPerUser {
		return model.Holding{}, appErrors.NewLimitExceededError(fmt.Sprintf(
			"You can track up to %d holdings. Remove one to add another.", model.MaxHoldingsPerUser))
	}

	// The user's existing spelling of the platform wins (matched case-insensitively), else it's created.
	holding.Platform, err = s.platformRepo.EnsureExists(ctx, command.UserId, platform, now)
	if err != nil {
		return model.Holding{}, err
	}

	return s.holdingRepo.Save(ctx, holding)
}

func (s *HoldingService) DeleteHolding(
	ctx context.Context,
	userId model.UserId,
	id model.HoldingId,
) error {
	deleted, err := s.holdingRepo.DeleteById(ctx, userId, id)
	if err != nil {
		return err
	}
	if !deleted {
		return appErrors.NewResourceNotFoundError(fmt.Sprintf("Holding %s not found", id.String()))
	}
	// There is no platform management in the UI: a platform lives exactly as long as a holding uses it.
	return s.platformRepo.DeleteUnused(ctx, userId)
}

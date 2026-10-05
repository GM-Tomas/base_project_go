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

var errHoldingsLimit = appErrors.NewLimitExceededError(fmt.Sprintf(
	"You can track up to %d holdings. Remove one to add another.", model.MaxHoldingsPerUser))

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

	// Validate everything before touching storage.
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
		return model.Holding{}, errHoldingsLimit
	}

	// The spelling the user's holdings already use for this platform wins (matched case-insensitively).
	holding.Platform, err = s.platformRepo.Canonical(ctx, command.UserId, platform)
	if err != nil {
		return model.Holding{}, err
	}

	saved, err := s.holdingRepo.Save(ctx, holding)
	if err != nil {
		return model.Holding{}, err
	}

	err = confirmUnderCap(ctx, model.MaxHoldingsPerUser, errHoldingsLimit,
		func(ctx context.Context) (int64, error) { return s.holdingRepo.Count(ctx, command.UserId) },
		func(ctx context.Context) error {
			_, err := s.holdingRepo.DeleteById(ctx, command.UserId, saved.Id)
			return err
		})
	if err != nil {
		return model.Holding{}, err
	}
	return saved, nil
}

// UpdateHolding changes what the command sends, validated like CreateHolding before anything is read. A
// platform is spelled as the user's holdings already spell it, as when creating; one the holding leaves
// without holdings is gone (platforms are derived from holdings). Sending what's already there writes
// nothing and keeps UpdatedAt.
func (s *HoldingService) UpdateHolding(
	ctx context.Context,
	command inbound.UpdateHoldingCommand,
) (model.Holding, error) {
	var (
		name       *string
		assetClass *model.AssetClass
		platform   *model.PlatformName
		value      *model.Money
	)
	if command.Name != nil {
		n, err := model.NormalizeHoldingName(*command.Name)
		if err != nil {
			return model.Holding{}, err
		}
		name = &n
	}
	if command.AssetClass != nil {
		ac, err := model.NewAssetClass(*command.AssetClass)
		if err != nil {
			return model.Holding{}, err
		}
		assetClass = &ac
	}
	if command.Platform != nil {
		p, err := model.NewPlatformName(*command.Platform)
		if err != nil {
			return model.Holding{}, err
		}
		platform = &p
	}
	if command.ValueUsd != nil {
		v, err := model.NewMoneyFromFloat(*command.ValueUsd)
		if err != nil {
			return model.Holding{}, err
		}
		value = &v
	}

	current, err := s.holdingRepo.FindById(ctx, command.UserId, command.Id)
	if err != nil {
		return model.Holding{}, err
	}
	if current == nil {
		return model.Holding{}, holdingNotFound(command.Id)
	}

	updated := *current
	if name != nil {
		updated.Name = *name
	}
	if assetClass != nil {
		updated.AssetClass = *assetClass
	}
	if value != nil {
		updated.Value = *value
	}
	if platform != nil {
		if updated.Platform, err = s.platformRepo.Canonical(ctx, command.UserId, *platform); err != nil {
			return model.Holding{}, err
		}
	}
	if updated.Name == current.Name && updated.AssetClass == current.AssetClass &&
		updated.Platform == current.Platform && updated.Value.Cmp(current.Value) == 0 {
		return *current, nil
	}

	updated.UpdatedAt = s.clock()
	found, err := s.holdingRepo.Update(ctx, updated)
	if err != nil {
		return model.Holding{}, err
	}
	if !found {
		return model.Holding{}, holdingNotFound(command.Id)
	}
	return updated, nil
}

func holdingNotFound(id model.HoldingId) error {
	return appErrors.NewResourceNotFoundError(fmt.Sprintf("Holding %s not found", id.String()))
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
		return holdingNotFound(id)
	}
	return nil
}

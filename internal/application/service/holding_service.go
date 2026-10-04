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
		return model.Holding{}, errHoldingsLimit
	}

	// The user's existing spelling of the platform wins (matched case-insensitively), else it's created.
	holding.Platform, err = s.platformRepo.EnsureExists(ctx, command.UserId, platform, now)
	if err != nil {
		return model.Holding{}, err
	}

	saved, err := s.holdingRepo.Save(ctx, holding)
	if err != nil {
		return model.Holding{}, err
	}

	// The count above isn't atomic with the insert, so concurrent creates can all pass it. Re-count with ours
	// in and withdraw it on an overrun: the last insert that stays has counted every other one that stays, so
	// the user never ends up above the cap. Fail-closed by design: when a burst crosses the cap every racer
	// may be withdrawn, even one that would have fit (a retry then succeeds), and ours is withdrawn too if the
	// re-count fails. Detached from the request, so a client hanging up right after the insert can't skip it.
	bg := context.WithoutCancel(ctx)
	count, err = s.holdingRepo.Count(bg, command.UserId)
	if err == nil && count <= model.MaxHoldingsPerUser {
		return saved, nil
	}
	if _, delErr := s.holdingRepo.DeleteById(bg, command.UserId, saved.Id); delErr != nil {
		return model.Holding{}, delErr
	}
	_ = s.platformRepo.DeleteUnused(bg, command.UserId) // best effort: at worst an empty platform lingers
	if err != nil {
		return model.Holding{}, err
	}
	return model.Holding{}, errHoldingsLimit
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

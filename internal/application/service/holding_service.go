package service

import (
	"context"
	"fmt"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/shopspring/decimal"
)

var errHoldingsLimit = appErrors.NewLimitExceededError(fmt.Sprintf(
	"You can track up to %d holdings. Remove one to add another.", model.MaxHoldingsPerUser))

type Clock func() time.Time

func RealClock() time.Time {
	return time.Now().UTC()
}

// HoldingService adds, edits and removes holdings. Each change of value is recorded in the activity log in
// the same transaction: an OPENING when one is added, a CLOSING when it's removed, and what an edit of its
// value was (see model.ValueChangeReason).
type HoldingService struct {
	tx           outbound.TransactionManager
	holdingRepo  outbound.HoldingRepository
	platformRepo outbound.PlatformRepository
	ledger       ledger
	clock        Clock
}

func NewHoldingService(
	tx outbound.TransactionManager,
	holdingRepo outbound.HoldingRepository,
	platformRepo outbound.PlatformRepository,
	movementRepo outbound.MovementRepository,
	quotaRepo outbound.QuotaRepository,
	clock Clock,
) *HoldingService {
	if clock == nil {
		clock = RealClock
	}
	return &HoldingService{
		tx:           tx,
		holdingRepo:  holdingRepo,
		platformRepo: platformRepo,
		ledger:       ledger{movements: movementRepo, quotas: quotaRepo},
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

// CreateHolding validates everything before touching storage, then adds the holding and its OPENING in one
// transaction. The holdings cap is checked in it too, and the OPENING counts against the user's movements,
// which serializes concurrent creates: the cap is exact.
func (s *HoldingService) CreateHolding(
	ctx context.Context,
	command inbound.CreateHoldingCommand,
) (model.Holding, error) {
	now := s.clock()

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

	holding, err := model.CreateHolding(command.UserId, command.Name, ac, platform, moneyVal, now)
	if err != nil {
		return model.Holding{}, err
	}
	if holding.ExpectedReturnPct, err = expectedReturn(command.ExpectedReturnPct); err != nil {
		return model.Holding{}, err
	}

	var saved model.Holding
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := holdingsUnderCap(ctx, s.holdingRepo, command.UserId); err != nil {
			return err
		}
		h := holding
		// The spelling the user's holdings already use for this platform wins (matched case-insensitively).
		if h.Platform, err = s.platformRepo.Canonical(ctx, command.UserId, platform); err != nil {
			return err
		}
		if saved, err = s.holdingRepo.Save(ctx, h); err != nil {
			return err
		}
		return s.ledger.record(ctx, lifecycleMovement(model.MovementOpening, saved, now))
	})
	if err != nil {
		return model.Holding{}, err
	}
	return saved, nil
}

// expectedReturn is a yearly return as sent (nil: none), checked.
func expectedReturn(sent *float64) (*decimal.Decimal, error) {
	if sent == nil {
		return nil, nil
	}
	pct, err := model.NewExpectedReturnPct(*sent)
	if err != nil {
		return nil, err
	}
	return &pct, nil
}

// lifecycleMovement is a holding being added (OPENING) or removed (CLOSING), with its value then.
func lifecycleMovement(kind model.MovementKind, h model.Holding, now time.Time) model.Movement {
	ref := model.RefOf(h)
	return model.Movement{
		Id: model.NewMovementId(), UserId: h.UserId, Kind: kind, OccurredAt: now,
		Amount: h.Value, Fee: model.ZeroMoney, Holding: &ref, CreatedAt: now,
	}
}

// UpdateHolding changes what the command sends, validated like CreateHolding before anything is read. A
// platform is spelled as the user's holdings already spell it, as when creating; one the holding leaves
// without holdings is gone (platforms are derived from holdings). A new value is recorded as a movement,
// computed against the value read in the same transaction (an edit from another device in between isn't
// lost). Sending what's already there writes nothing and keeps UpdatedAt.
func (s *HoldingService) UpdateHolding(
	ctx context.Context,
	command inbound.UpdateHoldingCommand,
) (model.Holding, error) {
	now := s.clock()
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
	newReturn, err := expectedReturn(command.ExpectedReturnPct.Value)
	if err != nil {
		return model.Holding{}, err
	}
	reason, err := model.ParseValueChangeReason(command.ValueChangeReason)
	if err != nil {
		return model.Holding{}, err
	}
	note, err := model.NormalizeNote(command.Note)
	if err != nil {
		return model.Holding{}, err
	}
	occurredAt := now
	if command.OccurredAt != nil {
		occurredAt = command.OccurredAt.UTC()
	}
	if err := model.CheckOccurredAt(occurredAt, now); err != nil {
		return model.Holding{}, err
	}

	var result model.Holding
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		current, err := findHolding(ctx, s.holdingRepo, command.UserId, command.Id)
		if err != nil {
			return err
		}
		updated := current
		if name != nil {
			updated.Name = *name
		}
		if assetClass != nil {
			updated.AssetClass = *assetClass
		}
		if value != nil {
			updated.Value = *value
		}
		if command.ExpectedReturnPct.Set {
			updated.ExpectedReturnPct = newReturn
		}
		if platform != nil {
			if updated.Platform, err = s.platformRepo.Canonical(ctx, command.UserId, *platform); err != nil {
				return err
			}
		}
		valueChanged := updated.Value.Cmp(current.Value) != 0
		if !valueChanged && updated.Name == current.Name && updated.AssetClass == current.AssetClass &&
			updated.Platform == current.Platform && model.SameReturn(updated.ExpectedReturnPct, current.ExpectedReturnPct) {
			result = current
			return nil
		}

		updated.UpdatedAt = now
		if err := updateHolding(ctx, s.holdingRepo, updated); err != nil {
			return err
		}
		result = updated
		if !valueChanged {
			return nil
		}
		ref := model.RefOf(updated)
		previous, next := current.Value, updated.Value
		delta := next.Amount().Sub(previous.Amount()).Abs()
		return s.ledger.record(ctx, model.Movement{
			Id: model.NewMovementId(), UserId: command.UserId, Kind: reason.KindFor(previous, next),
			OccurredAt: occurredAt, Amount: model.MustMoney(delta), Fee: model.ZeroMoney, Holding: &ref,
			PreviousValue: &previous, NewValue: &next, Note: note, CreatedAt: now,
		})
	})
	if err != nil {
		return model.Holding{}, err
	}
	return result, nil
}

func holdingNotFound(id model.HoldingId) error {
	return appErrors.NewResourceNotFoundError(fmt.Sprintf("Holding %s not found", id.String()))
}

// DeleteHolding removes the holding and records its CLOSING, with the value it had, in one transaction. Its
// movements stay in the activity log, naming it as it was.
func (s *HoldingService) DeleteHolding(
	ctx context.Context,
	userId model.UserId,
	id model.HoldingId,
) error {
	now := s.clock()
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := findHolding(ctx, s.holdingRepo, userId, id)
		if err != nil {
			return err
		}
		deleted, err := s.holdingRepo.DeleteById(ctx, userId, id)
		if err != nil {
			return err
		}
		if !deleted {
			return holdingNotFound(id)
		}
		return s.ledger.record(ctx, lifecycleMovement(model.MovementClosing, h, now))
	})
}

// SetExpectedReturns checks every return first, then sets them in one transaction against the holdings read
// in it: a holding that isn't the user's fails it all (404). Returns that don't change write nothing (their
// holding keeps its UpdatedAt); the result is each holding as it ends up, in the order asked.
func (s *HoldingService) SetExpectedReturns(
	ctx context.Context,
	userId model.UserId,
	items []inbound.ExpectedReturnItem,
) ([]model.Holding, error) {
	now := s.clock()
	returns := make([]*decimal.Decimal, len(items))
	for i, item := range items {
		pct, err := expectedReturn(item.ExpectedReturnPct)
		if err != nil {
			return nil, err
		}
		returns[i] = pct
	}

	var result []model.Holding
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		holdings, err := s.holdingRepo.FindAll(ctx, userId)
		if err != nil {
			return err
		}
		byId := make(map[model.HoldingId]model.Holding, len(holdings))
		for _, h := range holdings {
			byId[h.Id] = h
		}
		changes := make(map[model.HoldingId]*decimal.Decimal)
		result = make([]model.Holding, len(items))
		for i, item := range items {
			h, ok := byId[item.HoldingId]
			if !ok {
				return holdingNotFound(item.HoldingId)
			}
			if !model.SameReturn(h.ExpectedReturnPct, returns[i]) {
				h.ExpectedReturnPct, h.UpdatedAt = returns[i], now
				byId[h.Id] = h
				changes[h.Id] = returns[i]
			}
		}
		// The same holding asked twice ends up as asked last.
		for i, item := range items {
			result[i] = byId[item.HoldingId]
		}
		if len(changes) == 0 {
			return nil
		}
		found, err := s.holdingRepo.SetExpectedReturns(ctx, userId, changes, now)
		if err != nil {
			return err
		}
		if found != len(changes) {
			return appErrors.NewResourceNotFoundError("Some of these holdings were removed meanwhile")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

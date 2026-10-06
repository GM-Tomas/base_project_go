package outbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

type HoldingRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.Holding, error)
	// FindById is the user's holding with that id, or nil if there's none (someone else's included).
	FindById(ctx context.Context, userId model.UserId, id model.HoldingId) (*model.Holding, error)
	Count(ctx context.Context, userId model.UserId) (int64, error)
	Save(ctx context.Context, holding model.Holding) (model.Holding, error)
	// Update replaces a holding that still exists; false if it's gone (or never was the user's), so an
	// edit racing a delete can't bring the holding back.
	Update(ctx context.Context, holding model.Holding) (bool, error)
	DeleteById(ctx context.Context, userId model.UserId, id model.HoldingId) (bool, error)
	AssetClassesInUse(ctx context.Context, userId model.UserId) ([]model.AssetClass, error)
	// ExistingIds says which of these holdings the user still has.
	ExistingIds(ctx context.Context, userId model.UserId, ids []model.HoldingId) (map[model.HoldingId]bool, error)
	// SetExpectedReturns sets each of these holdings' expected return, and its UpdatedAt, leaving the rest
	// as stored; it says how many of them the user has.
	SetExpectedReturns(ctx context.Context, userId model.UserId, returns map[model.HoldingId]*decimal.Decimal,
		updatedAt time.Time) (int, error)
	// ReassignAssetClass moves the user's holdings of class from (as the domain reads their classes) to
	// class to, touching nothing else of them; it says how many it moved.
	ReassignAssetClass(ctx context.Context, userId model.UserId, from, to model.AssetClass) (int, error)
	// ReassignPlatform names the platform of the user's holdings on the one with this key (see
	// model.PlatformKey) to, touching nothing else of them; it says how many it renamed.
	ReassignPlatform(ctx context.Context, userId model.UserId, key string, to model.PlatformName) (int, error)
}

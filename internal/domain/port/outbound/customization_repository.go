package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// AssetClassSettingsRepository keeps what users set for their asset classes, one document per class.
type AssetClassSettingsRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.AssetClassSettings, error)
	// Save creates or replaces the user's settings for the class.
	Save(ctx context.Context, settings model.AssetClassSettings) error
	// Delete removes them; false if there were none.
	Delete(ctx context.Context, userId model.UserId, class model.AssetClass) (bool, error)
}

// PlatformSettingsRepository keeps how users set up their platforms, one document per platform key.
type PlatformSettingsRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.PlatformSettings, error)
	// Find is the user's settings for the platform with that key, or nil.
	Find(ctx context.Context, userId model.UserId, key string) (*model.PlatformSettings, error)
	// Save creates or replaces the user's settings for the platform.
	Save(ctx context.Context, settings model.PlatformSettings) error
	// Delete removes them; false if there were none.
	Delete(ctx context.Context, userId model.UserId, key string) (bool, error)
}

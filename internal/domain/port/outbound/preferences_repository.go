package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type PreferencesRepository interface {
	// Find is what the user saved, or nil if they never did.
	Find(ctx context.Context, userId model.UserId) (*model.Preferences, error)
	// Save replaces what the user saved.
	Save(ctx context.Context, userId model.UserId, preferences model.Preferences) error
}

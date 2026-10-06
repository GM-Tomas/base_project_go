package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// PlatformRepository is read-only: a platform is a name the user's holdings use (matched
// case-insensitively), so it appears with the first holding on it and is gone with the last.
type PlatformRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.Platform, error)
	// Canonical is the spelling the user's holdings already use for this platform, or name for a new one.
	Canonical(ctx context.Context, userId model.UserId, name model.PlatformName) (model.PlatformName, error)
	// Names is each of the user's platforms by key (see model.PlatformKey), spelled as FindAll spells it:
	// one read, so it can be used inside a transaction.
	Names(ctx context.Context, userId model.UserId) (map[string]model.PlatformName, error)
}

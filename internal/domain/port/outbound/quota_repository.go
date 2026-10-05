package outbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// QuotaRepository counts what a user has of something with a per-user cap, inside the transaction that
// adds or removes it. Every reservation writes the user's counter, so two transactions reserving for the
// same user conflict and one is retried: the count stays exact under concurrent requests.
type QuotaRepository interface {
	// Reserve counts n more of key if that keeps it within limit; false, with nothing counted, if not.
	Reserve(ctx context.Context, userId model.UserId, key string, n, limit int) (bool, error)
	// Release counts n fewer of key.
	Release(ctx context.Context, userId model.UserId, key string, n int) error
}

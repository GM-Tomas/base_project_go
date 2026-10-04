package outbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type SnapshotRepository interface {
	FindAll(ctx context.Context, userId model.UserId) ([]model.NetWorthSnapshot, error)
	Count(ctx context.Context, userId model.UserId) (int64, error)
	Save(ctx context.Context, snapshot model.NetWorthSnapshot) (model.NetWorthSnapshot, error)
	DeleteById(ctx context.Context, userId model.UserId, id model.SnapshotId) (bool, error)
	ExistsAt(ctx context.Context, userId model.UserId, capturedAt time.Time) (bool, error)
	FindFirstOfYear(ctx context.Context, userId model.UserId, year int) (*model.NetWorthSnapshot, error)
	FindEarliest(ctx context.Context, userId model.UserId) (*model.NetWorthSnapshot, error)
}

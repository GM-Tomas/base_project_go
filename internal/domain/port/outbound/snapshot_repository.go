package outbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type SnapshotRepository interface {
	FindAll(ctx context.Context, userId model.UserId, from *time.Time, to *time.Time) ([]model.NetWorthSnapshot, error)
	Save(ctx context.Context, snapshot model.NetWorthSnapshot) (model.NetWorthSnapshot, error)
	ExistsAt(ctx context.Context, userId model.UserId, capturedAt time.Time) (bool, error)
	FindFirstOfYear(ctx context.Context, userId model.UserId, year int) (*model.NetWorthSnapshot, error)
	FindEarliest(ctx context.Context, userId model.UserId) (*model.NetWorthSnapshot, error)
}

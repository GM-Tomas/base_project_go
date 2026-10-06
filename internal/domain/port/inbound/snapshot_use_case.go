package inbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

type SnapshotWithChange struct {
	Snapshot              model.NetWorthSnapshot
	ChangePctFromPrevious *decimal.Decimal
}

// ManualSnapshotCommand is a past net worth the user enters (see model.NewManualSnapshot): what they owned
// and owed, both or neither.
type ManualSnapshotCommand struct {
	UserId        model.UserId
	CapturedAt    time.Time
	TotalValueUsd float64
	AssetsUsd     *float64
	DebtsUsd      *float64
	Note          string
}

type SnapshotUseCase interface {
	CreateSnapshot(ctx context.Context, userId model.UserId) (model.NetWorthSnapshot, error)
	// CreateManualSnapshot keeps a past net worth, as CreateSnapshot keeps today's: one per second, within
	// the cap.
	CreateManualSnapshot(ctx context.Context, command ManualSnapshotCommand) (model.NetWorthSnapshot, error)
	GetSnapshots(ctx context.Context, userId model.UserId) ([]SnapshotWithChange, error)
	DeleteSnapshot(ctx context.Context, userId model.UserId, id model.SnapshotId) error
}

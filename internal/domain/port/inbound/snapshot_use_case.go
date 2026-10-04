package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
)

type SnapshotWithChange struct {
	Snapshot              model.NetWorthSnapshot
	ChangePctFromPrevious *decimal.Decimal
}

type SnapshotUseCase interface {
	CreateSnapshot(ctx context.Context, userId model.UserId) (model.NetWorthSnapshot, error)
	GetSnapshots(ctx context.Context, userId model.UserId) ([]SnapshotWithChange, error)
}

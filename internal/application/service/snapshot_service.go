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

type SnapshotService struct {
	snapshotRepo          outbound.SnapshotRepository
	wealthAggregationPort outbound.WealthAggregationPort
	clock                 Clock
}

func NewSnapshotService(
	snapshotRepo outbound.SnapshotRepository,
	wealthAggregationPort outbound.WealthAggregationPort,
	clock Clock,
) *SnapshotService {
	if clock == nil {
		clock = RealClock
	}
	return &SnapshotService{
		snapshotRepo:          snapshotRepo,
		wealthAggregationPort: wealthAggregationPort,
		clock:                 clock,
	}
}

var _ inbound.SnapshotUseCase = (*SnapshotService)(nil)

func (s *SnapshotService) CreateSnapshot(
	ctx context.Context,
	userId model.UserId,
) (model.NetWorthSnapshot, error) {
	capturedAt := s.clock().Truncate(time.Second)

	exists, err := s.snapshotRepo.ExistsAt(ctx, userId, capturedAt)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}
	if exists {
		return model.NetWorthSnapshot{}, appErrors.NewDuplicateResourceError(
			fmt.Sprintf("A snapshot already exists for %s", capturedAt.Format(time.RFC3339)),
		)
	}

	count, err := s.snapshotRepo.Count(ctx, userId)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}
	if count >= model.MaxSnapshotsPerUser {
		return model.NetWorthSnapshot{}, appErrors.NewLimitExceededError(fmt.Sprintf(
			"You've reached the limit of %d snapshots.", model.MaxSnapshotsPerUser))
	}

	netWorth, err := s.wealthAggregationPort.NetWorth(ctx, userId)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}

	snapshot := model.NewNetWorthSnapshot(model.NewSnapshotId(), userId, capturedAt, netWorth)
	return s.snapshotRepo.Save(ctx, snapshot)
}

func (s *SnapshotService) GetSnapshots(
	ctx context.Context,
	userId model.UserId,
) ([]inbound.SnapshotWithChange, error) {
	snapshots, err := s.snapshotRepo.FindAll(ctx, userId)
	if err != nil {
		return nil, err
	}

	result := make([]inbound.SnapshotWithChange, len(snapshots))
	for i, snap := range snapshots {
		var decimalGrowth *decimal.Decimal
		if i > 0 {
			prev := snapshots[i-1]
			decimalGrowth = snap.TotalValue.GrowthPctFrom(prev.TotalValue)
		}

		result[i] = inbound.SnapshotWithChange{
			Snapshot:              snap,
			ChangePctFromPrevious: decimalGrowth,
		}
	}

	return result, nil
}

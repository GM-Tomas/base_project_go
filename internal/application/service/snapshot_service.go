package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
	appErrors "github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/shopspring/decimal"
)

var errSnapshotsLimit = appErrors.NewLimitExceededError(fmt.Sprintf(
	"You've reached the limit of %d snapshots.", model.MaxSnapshotsPerUser))

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

// CreateSnapshot keeps the user's net worth now: what they own and owe, read as the summary reads them.
func (s *SnapshotService) CreateSnapshot(
	ctx context.Context,
	userId model.UserId,
) (model.NetWorthSnapshot, error) {
	capturedAt := s.clock().Truncate(time.Second)
	if err := s.roomAt(ctx, userId, capturedAt); err != nil {
		return model.NetWorthSnapshot{}, err
	}

	totals, err := s.wealthAggregationPort.Totals(ctx, userId)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}
	return s.save(ctx, model.NewNetWorthSnapshot(model.NewSnapshotId(), userId, capturedAt, totals.Assets, totals.Debts))
}

// CreateManualSnapshot checks a past net worth as the domain does (model.NewManualSnapshot), then keeps it
// under the same rules as today's.
func (s *SnapshotService) CreateManualSnapshot(
	ctx context.Context,
	cmd inbound.ManualSnapshotCommand,
) (model.NetWorthSnapshot, error) {
	if math.IsNaN(cmd.TotalValueUsd) || math.IsInf(cmd.TotalValueUsd, 0) {
		return model.NetWorthSnapshot{}, model.ErrNonFiniteMoney
	}
	var assets, debts *model.Money
	for _, part := range []struct {
		sent *float64
		dst  **model.Money
	}{{cmd.AssetsUsd, &assets}, {cmd.DebtsUsd, &debts}} {
		if part.sent == nil {
			continue
		}
		m, err := model.NewMoneyFromFloat(*part.sent)
		if err != nil {
			return model.NetWorthSnapshot{}, err
		}
		*part.dst = &m
	}
	snapshot, err := model.NewManualSnapshot(model.NewSnapshotId(), cmd.UserId, cmd.CapturedAt,
		decimal.NewFromFloat(cmd.TotalValueUsd), assets, debts, cmd.Note, s.clock())
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}
	if err := s.roomAt(ctx, cmd.UserId, snapshot.CapturedAt); err != nil {
		return model.NetWorthSnapshot{}, err
	}
	return s.save(ctx, snapshot)
}

// roomAt checks there's no snapshot at that second yet, and room for one more.
func (s *SnapshotService) roomAt(ctx context.Context, userId model.UserId, capturedAt time.Time) error {
	exists, err := s.snapshotRepo.ExistsAt(ctx, userId, capturedAt)
	if err != nil {
		return err
	}
	if exists {
		return appErrors.NewDuplicateResourceError(
			fmt.Sprintf("A snapshot already exists for %s", capturedAt.Format(time.RFC3339)),
		)
	}
	count, err := s.snapshotRepo.Count(ctx, userId)
	if err != nil {
		return err
	}
	if count >= model.MaxSnapshotsPerUser {
		return errSnapshotsLimit
	}
	return nil
}

// save stores the snapshot, then makes sure a concurrent one didn't take the last place (see
// confirmUnderCap).
func (s *SnapshotService) save(ctx context.Context, snapshot model.NetWorthSnapshot) (model.NetWorthSnapshot, error) {
	saved, err := s.snapshotRepo.Save(ctx, snapshot)
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}
	err = confirmUnderCap(ctx, model.MaxSnapshotsPerUser, errSnapshotsLimit,
		func(ctx context.Context) (int64, error) { return s.snapshotRepo.Count(ctx, snapshot.UserId) },
		func(ctx context.Context) error {
			_, err := s.snapshotRepo.DeleteById(ctx, snapshot.UserId, saved.Id)
			return err
		})
	if err != nil {
		return model.NetWorthSnapshot{}, err
	}
	return saved, nil
}

func (s *SnapshotService) GetSnapshots(
	ctx context.Context,
	userId model.UserId,
) ([]inbound.SnapshotWithChange, error) {
	snapshots, err := s.snapshotRepo.FindAll(ctx, userId)
	if err != nil {
		return nil, err
	}

	// Each one's change is of the net worth, from the one before; none when that one wasn't above zero.
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

// DeleteSnapshot removes one of the user's snapshots. The change shown on the next one is computed on read
// (GetSnapshots), so it's then measured against the one before the deleted one.
func (s *SnapshotService) DeleteSnapshot(ctx context.Context, userId model.UserId, id model.SnapshotId) error {
	deleted, err := s.snapshotRepo.DeleteById(ctx, userId, id)
	if err != nil {
		return err
	}
	if !deleted {
		return appErrors.NewResourceNotFoundError(fmt.Sprintf("Snapshot %s not found", id.String()))
	}
	return nil
}

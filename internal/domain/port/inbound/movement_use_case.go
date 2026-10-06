package inbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// NewHoldingInput is a holding a transfer creates as its destination.
type NewHoldingInput struct {
	Name       string
	AssetClass string
	Platform   string
}

// RecordMovementCommand is a movement a user records: a gain, loss, deposit or withdrawal on HoldingId; a
// transfer from FromHoldingId to ToHoldingId or to a new holding; or a debt's payment (from FromHoldingId,
// if any), charge (into ToHoldingId, if any) or interest, on DebtId.
type RecordMovementCommand struct {
	UserId        model.UserId
	Kind          string
	HoldingId     *model.HoldingId
	FromHoldingId *model.HoldingId
	ToHoldingId   *model.HoldingId
	DebtId        *model.DebtId
	ToNewHolding  *NewHoldingInput
	AmountUsd     float64
	FeeUsd        float64
	OccurredAt    *time.Time // nil: now
	Note          string
}

// MovementView is a movement as the activity log shows it: whether the holdings and the debt it names still
// exist, and so whether it can still be undone.
type MovementView struct {
	Movement        model.Movement
	HoldingExists   bool
	ToHoldingExists bool
	DebtExists      bool
	Revertible      bool
}

type MovementQuery struct {
	Filter model.MovementFilter
	After  *model.MovementCursor
	Limit  int
}

type MovementList struct {
	Items []MovementView
	Next  *model.MovementCursor
}

// MovementsSummaryResult is what the movements of [From, To] add up to.
type MovementsSummaryResult struct {
	From, To time.Time
	Summary  model.MovementsSummary
}

type MovementUseCase interface {
	RecordMovement(ctx context.Context, command RecordMovementCommand) (MovementView, error)
	// SummarizeMovements adds up the movements that happened within [from, to] (nil: since 1970, until now)
	// by bucket, and what they did to the net worth.
	SummarizeMovements(ctx context.Context, userId model.UserId, from, to *time.Time) (MovementsSummaryResult, error)
	ListMovements(ctx context.Context, userId model.UserId, query MovementQuery) (MovementList, error)
	// RevertMovement undoes a movement's effect on the holdings and the debt it touched, as deltas, and
	// deletes it.
	RevertMovement(ctx context.Context, userId model.UserId, id model.MovementId) error
}

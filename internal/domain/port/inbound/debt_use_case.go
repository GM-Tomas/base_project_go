package inbound

import (
	"context"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

// Change is an optional field a PATCH may set: nothing changes unless Set; a nil Value clears it.
type Change[T any] struct {
	Set   bool
	Value *T
}

type CreateDebtCommand struct {
	UserId            model.UserId
	Name              string
	Lender            string
	Kind              string // "" is OTHER
	BalanceUsd        float64
	InterestRatePct   *float64
	MonthlyPaymentUsd *float64
	DueDay            *int
	Notes             string
}

// UpdateDebtCommand changes what's set and leaves the rest as it is. Lender and Notes are cleared with "".
// A change of balance is recorded as a movement: BalanceChangeReason says which ("" is a correction),
// OccurredAt when (nil: now).
type UpdateDebtCommand struct {
	UserId              model.UserId
	Id                  model.DebtId
	Name                *string
	Lender              *string
	Kind                *string
	BalanceUsd          *float64
	InterestRatePct     Change[float64]
	MonthlyPaymentUsd   Change[float64]
	DueDay              Change[int]
	Notes               *string
	BalanceChangeReason string
	OccurredAt          *time.Time
	Note                string
}

// DebtView is a debt and when it gets paid off at its monthly payment.
type DebtView struct {
	Debt   model.Debt
	Payoff model.DebtPayoff
}

type DebtUseCase interface {
	GetDebts(ctx context.Context, userId model.UserId) ([]DebtView, error)
	CreateDebt(ctx context.Context, command CreateDebtCommand) (DebtView, error)
	UpdateDebt(ctx context.Context, command UpdateDebtCommand) (DebtView, error)
	// DeleteDebt removes a debt, recording its CLOSING; its movements stay in the activity log.
	DeleteDebt(ctx context.Context, userId model.UserId, id model.DebtId) error
}

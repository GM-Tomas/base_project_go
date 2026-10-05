package dto

import "time"

// CreateDebtRequest is POST /debts. Only name and balanceUsd are required; kind is OTHER when not sent.
type CreateDebtRequest struct {
	Name              string   `json:"name"`
	Lender            string   `json:"lender"`
	Kind              string   `json:"kind"`
	BalanceUsd        *float64 `json:"balanceUsd"`
	InterestRatePct   *float64 `json:"interestRatePct"`
	MonthlyPaymentUsd *float64 `json:"monthlyPaymentUsd"`
	DueDay            *int     `json:"dueDay"`
	Notes             string   `json:"notes"`
}

// UpdateDebtRequest is PATCH /debts/{id}, a JSON merge patch: only the fields sent change, and null clears
// the optional ones (lender, interestRatePct, monthlyPaymentUsd, dueDay, notes). A new balanceUsd is recorded
// as a movement: balanceChangeReason says what it was (CORRECTION by default), occurredAt and note go with
// it.
type UpdateDebtRequest struct {
	Name                Optional[string]  `json:"name"`
	Lender              Optional[string]  `json:"lender"`
	Kind                Optional[string]  `json:"kind"`
	BalanceUsd          Optional[float64] `json:"balanceUsd"`
	InterestRatePct     Optional[float64] `json:"interestRatePct"`
	MonthlyPaymentUsd   Optional[float64] `json:"monthlyPaymentUsd"`
	DueDay              Optional[int]     `json:"dueDay"`
	Notes               Optional[string]  `json:"notes"`
	BalanceChangeReason string            `json:"balanceChangeReason"`
	OccurredAt          string            `json:"occurredAt"`
	Note                string            `json:"note"`
}

// DebtPayoffResponse is when a debt is paid off at its monthly payment: months, payoffMonth and
// totalInterestUsd are set when status is ON_TRACK.
type DebtPayoffResponse struct {
	Status           string   `json:"status"`
	Months           *int     `json:"months"`
	PayoffMonth      *string  `json:"payoffMonth"`
	TotalInterestUsd *float64 `json:"totalInterestUsd"`
}

type DebtResponse struct {
	Id                string             `json:"id"`
	Name              string             `json:"name"`
	Lender            *string            `json:"lender"`
	Kind              string             `json:"kind"`
	BalanceUsd        float64            `json:"balanceUsd"`
	InterestRatePct   *float64           `json:"interestRatePct"`
	MonthlyPaymentUsd *float64           `json:"monthlyPaymentUsd"`
	DueDay            *int               `json:"dueDay"`
	Notes             *string            `json:"notes"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
	Payoff            DebtPayoffResponse `json:"payoff"`
}

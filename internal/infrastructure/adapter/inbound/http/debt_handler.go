package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/go-chi/chi/v5"
)

const (
	maxInterestRatePct = 200.0
	kindMessage        = "kind must be one of CREDIT_CARD, LOAN, MORTGAGE, PERSONAL, OTHER"
)

type DebtHandler struct {
	debtUseCase inbound.DebtUseCase
}

func NewDebtHandler(debtUseCase inbound.DebtUseCase) *DebtHandler {
	return &DebtHandler{debtUseCase: debtUseCase}
}

// debtChecks collects a debt body's problems, field by field, with the messages the API answers.
type debtChecks struct {
	problems []errors.ValidationError
}

func (c *debtChecks) fail(field, message string) {
	c.problems = append(c.problems, errors.ValidationError{Field: field, Message: message})
}

func (c *debtChecks) name(v string) {
	if strings.TrimSpace(v) == "" {
		c.fail("name", "Name is required")
	}
}

func (c *debtChecks) kind(v string) {
	if _, err := model.ParseDebtKind(v); err != nil {
		c.fail("kind", kindMessage)
	}
}

func (c *debtChecks) balance(v float64) {
	if v < 0 {
		c.fail("balanceUsd", "Balance must not be negative")
	} else if v > maxHoldingValueUsd {
		c.fail("balanceUsd", "Balance is too large")
	}
}

func (c *debtChecks) rate(v float64) {
	if !inRange(v, 0, maxInterestRatePct) {
		c.fail("interestRatePct", "interestRatePct must be between 0 and 200")
	}
}

func (c *debtChecks) payment(v float64) {
	if v < 0 {
		c.fail("monthlyPaymentUsd", "Monthly payment must not be negative")
	} else if v > maxHoldingValueUsd {
		c.fail("monthlyPaymentUsd", "Monthly payment is too large")
	}
}

func (c *debtChecks) dueDay(v int) {
	if v < 1 || v > model.MaxDebtDueDay {
		c.fail("dueDay", "dueDay must be between 1 and 31")
	}
}

func (h *DebtHandler) GetDebts(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	debts, err := h.debtUseCase.GetDebts(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	res := make([]dto.DebtResponse, len(debts))
	for i, d := range debts {
		res[i] = toDebtResponse(d)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *DebtHandler) CreateDebt(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	var req dto.CreateDebtRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	var c debtChecks
	c.name(req.Name)
	c.kind(req.Kind)
	if req.BalanceUsd == nil {
		c.fail("balanceUsd", "Balance is required")
	} else {
		c.balance(*req.BalanceUsd)
	}
	if req.InterestRatePct != nil {
		c.rate(*req.InterestRatePct)
	}
	if req.MonthlyPaymentUsd != nil {
		c.payment(*req.MonthlyPaymentUsd)
	}
	if req.DueDay != nil {
		c.dueDay(*req.DueDay)
	}
	if len(c.problems) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(c.problems))
		return
	}

	created, err := h.debtUseCase.CreateDebt(r.Context(), inbound.CreateDebtCommand{
		UserId: userId, Name: req.Name, Lender: req.Lender, Kind: req.Kind, BalanceUsd: *req.BalanceUsd,
		InterestRatePct: req.InterestRatePct, MonthlyPaymentUsd: req.MonthlyPaymentUsd, DueDay: req.DueDay,
		Notes: req.Notes,
	})
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/debts/"+created.Debt.Id.String())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toDebtResponse(created))
}

// change is an optional term of a PATCH: null clears it, a value is checked by check.
func change[T any](sent dto.Optional[T], check func(T)) inbound.Change[T] {
	if !sent.Set {
		return inbound.Change[T]{}
	}
	if sent.Null {
		return inbound.Change[T]{Set: true}
	}
	check(sent.Value)
	return inbound.Change[T]{Set: true, Value: &sent.Value}
}

// UpdateDebt is PATCH /debts/{id}: the fields sent change, with the checks and messages of CreateDebt. Name,
// kind and balance are required on a debt, so null is a validation error for them; for the rest, it clears.
func (h *DebtHandler) UpdateDebt(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	idStr := chi.URLParam(r, "id")
	debtId, err := model.ParseDebtId(idStr)
	if err != nil {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("Debt "+idStr+" not found"))
		return
	}
	var req dto.UpdateDebtRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	var c debtChecks
	cmd := inbound.UpdateDebtCommand{
		UserId: userId, Id: debtId, BalanceChangeReason: req.BalanceChangeReason, Note: req.Note,
	}
	if sent := req.Name; sent.Set {
		c.name(sent.Value) // null reads as blank: required
		cmd.Name = &sent.Value
	}
	if sent := req.Kind; sent.Set {
		if sent.Null {
			c.fail("kind", "Kind is required")
		} else {
			c.kind(sent.Value)
			cmd.Kind = &sent.Value
		}
	}
	if sent := req.BalanceUsd; sent.Set {
		if sent.Null {
			c.fail("balanceUsd", "Balance is required")
		} else {
			c.balance(sent.Value)
			cmd.BalanceUsd = &sent.Value
		}
	}
	if sent := req.Lender; sent.Set {
		cmd.Lender = &sent.Value // null clears: ""
	}
	if sent := req.Notes; sent.Set {
		cmd.Notes = &sent.Value
	}
	cmd.InterestRatePct = change(req.InterestRatePct, c.rate)
	cmd.MonthlyPaymentUsd = change(req.MonthlyPaymentUsd, c.payment)
	cmd.DueDay = change(req.DueDay, c.dueDay)
	if req.OccurredAt != "" {
		at, ok := parseWhen(req.OccurredAt, noon)
		if !ok {
			c.fail("occurredAt", "occurredAt must be a date (YYYY-MM-DD) or a date and time (RFC 3339)")
		}
		cmd.OccurredAt = &at
	}
	if len(c.problems) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(c.problems))
		return
	}

	updated, err := h.debtUseCase.UpdateDebt(r.Context(), cmd)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toDebtResponse(updated))
}

func (h *DebtHandler) DeleteDebt(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	idStr := chi.URLParam(r, "id")
	debtId, err := model.ParseDebtId(idStr)
	if err != nil {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("Debt "+idStr+" not found"))
		return
	}
	if err := h.debtUseCase.DeleteDebt(r.Context(), userId, debtId); err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// optionalText is a text field that's null when empty.
func optionalText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toDebtResponse(v inbound.DebtView) dto.DebtResponse {
	d := v.Debt
	res := dto.DebtResponse{
		Id:                d.Id.String(),
		Name:              d.Name,
		Lender:            optionalText(d.Lender),
		Kind:              string(d.Kind),
		BalanceUsd:        d.Balance.Float64(),
		MonthlyPaymentUsd: optionalUsd(d.MonthlyPayment),
		DueDay:            d.DueDay,
		Notes:             optionalText(d.Notes),
		CreatedAt:         d.CreatedAt,
		UpdatedAt:         d.UpdatedAt,
		Payoff: dto.DebtPayoffResponse{
			Status:           string(v.Payoff.Status),
			Months:           v.Payoff.Months,
			PayoffMonth:      v.Payoff.PayoffMonth,
			TotalInterestUsd: optionalUsd(v.Payoff.TotalInterest),
		},
	}
	if d.InterestRatePct != nil {
		rate := d.InterestRatePct.InexactFloat64()
		res.InterestRatePct = &rate
	}
	return res
}

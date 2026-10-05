package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/go-chi/chi/v5"
)

// The Estimate view shows exactly two milestones ("next" and "bigger").
var estimateMilestones = []float64{150000.0, 250000.0}

const maxMonthlyContributionUsd = 1000000000.0

// inRange also rejects NaN: strconv.ParseFloat accepts "NaN", every < / > comparison lets it
// through, and decimal.NewFromFloat panics on it.
func inRange(v, min, max float64) bool {
	return v >= min && v <= max
}

type WealthHandler struct {
	wealthUseCase     inbound.WealthUseCase
	snapshotUseCase   inbound.SnapshotUseCase
	projectionUseCase inbound.ProjectionUseCase
}

func NewWealthHandler(
	wealthUseCase inbound.WealthUseCase,
	snapshotUseCase inbound.SnapshotUseCase,
	projectionUseCase inbound.ProjectionUseCase,
) *WealthHandler {
	return &WealthHandler{
		wealthUseCase:     wealthUseCase,
		snapshotUseCase:   snapshotUseCase,
		projectionUseCase: projectionUseCase,
	}
}

func (h *WealthHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	summary, err := h.wealthUseCase.GetSummary(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(summary)
}

func (h *WealthHandler) GetEstimate(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	q := r.URL.Query()

	contributionStr := q.Get("contribution")
	if contributionStr == "" {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "contribution", Message: "contribution is required"},
		}))
		return
	}
	contribution, err := strconv.ParseFloat(contributionStr, 64)
	if err != nil || !inRange(contribution, 0, maxMonthlyContributionUsd) {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "contribution", Message: "contribution must be between 0 and 1000000000"},
		}))
		return
	}

	yieldPctStr := q.Get("yieldPct")
	if yieldPctStr == "" {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "yieldPct", Message: "yieldPct is required"},
		}))
		return
	}
	yieldPct, err := strconv.ParseFloat(yieldPctStr, 64)
	if err != nil || !inRange(yieldPct, 0, 100.0) {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "yieldPct", Message: "yieldPct must be between 0 and 100"},
		}))
		return
	}

	yearsStr := q.Get("years")
	if yearsStr == "" {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "years", Message: "years is required"},
		}))
		return
	}
	years, err := strconv.Atoi(yearsStr)
	if err != nil || years < 1 || years > model.MaxProjectionYears {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "years", Message: "years must be between 1 and 50"},
		}))
		return
	}

	req := inbound.ProjectionRequest{
		UserId:              userId,
		MonthlyContribution: contribution,
		AnnualYieldPct:      yieldPct,
		Years:               years,
		Milestones:          estimateMilestones,
	}

	result, err := h.projectionUseCase.Project(r.Context(), req)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	seriesRes := make([]dto.ProjectionPointResponse, len(result.Series))
	for i, pt := range result.Series {
		seriesRes[i] = dto.ProjectionPointResponse{
			Year:                pt.Year,
			FutureValueUsd:      pt.FutureValue.Float64(),
			TotalContributedUsd: pt.TotalContributed.Float64(),
			InterestEarnedUsd:   pt.InterestEarned.Float64(),
			DebtBalanceUsd:      pt.DebtBalance.Float64(),
			NetWorthUsd:         pt.NetWorth.Float64(),
		}
	}

	milestonesRes := make([]dto.MilestoneResponse, len(result.Milestones))
	for i, m := range result.Milestones {
		milestonesRes[i] = dto.MilestoneResponse{
			AmountUsd:      m.Amount.Float64(),
			Status:         string(m.Status),
			MonthsRequired: m.MonthsRequired,
			TargetMonth:    m.TargetMonth,
		}
	}

	annualYieldFloat, _ := result.AnnualYieldPct.Float64()
	response := dto.ProjectionResponse{
		PrincipalUsd:           result.Principal.Float64(),
		DebtsUsd:               result.Debts.Float64(),
		MonthlyContributionUsd: result.MonthlyContribution.Float64(),
		AnnualYieldPct:         annualYieldFloat,
		Years:                  result.Years,
		Series:                 seriesRes,
		Milestones:             milestonesRes,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func (h *WealthHandler) GetSnapshots(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	snapshots, err := h.snapshotUseCase.GetSnapshots(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	res := make([]dto.SnapshotResponse, len(snapshots))
	for i, s := range snapshots {
		var changePct *float64
		if s.ChangePctFromPrevious != nil {
			val, _ := s.ChangePctFromPrevious.Round(1).Float64()
			changePct = &val
		}
		res[i] = toSnapshotResponse(s.Snapshot)
		res[i].ChangePctFromPrevious = changePct
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *WealthHandler) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	snapshot, err := h.snapshotUseCase.CreateSnapshot(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	res := toSnapshotResponse(snapshot)

	w.Header().Set("Location", "/api/v1/wealth/snapshots/"+snapshot.Id.String())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *WealthHandler) DeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	idStr := chi.URLParam(r, "id")
	snapshotId, err := model.ParseSnapshotId(idStr)
	if err != nil {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("Snapshot "+idStr+" not found"))
		return
	}

	if err := h.snapshotUseCase.DeleteSnapshot(r.Context(), userId, snapshotId); err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toSnapshotResponse(s model.NetWorthSnapshot) dto.SnapshotResponse {
	return dto.SnapshotResponse{
		Id:            s.Id.String(),
		CapturedAt:    s.CapturedAt,
		TotalValueUsd: s.TotalValue.Float64(),
		AssetsUsd:     s.Assets.Float64(),
		DebtsUsd:      s.Debts.Float64(),
	}
}

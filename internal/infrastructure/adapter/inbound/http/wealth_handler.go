package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
)

var defaultEstimateMilestones = []float64{150000.0, 250000.0}

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
	if err != nil || contribution < 0 || contribution > 1000000000.0 {
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
	if err != nil || yieldPct < 0 || yieldPct > 100.0 {
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

	var milestones []float64
	milestonesQuery := q["milestones"]
	if len(milestonesQuery) > 0 {
		for _, param := range milestonesQuery {
			for _, part := range strings.Split(param, ",") {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					mVal, err := strconv.ParseFloat(trimmed, 64)
					if err != nil || mVal < 0 {
						middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
							{Field: "milestones", Message: "milestone amounts must be non-negative numbers"},
						}))
						return
					}
					milestones = append(milestones, mVal)
				}
			}
		}
	} else {
		milestones = defaultEstimateMilestones
	}

	if len(milestones) > model.MaxProjectionMilestones {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "milestones", Message: "at most 5 milestones are allowed"},
		}))
		return
	}

	var principalOverride *float64
	if princStr := q.Get("principal"); princStr != "" {
		pVal, err := strconv.ParseFloat(princStr, 64)
		if err != nil || pVal < 0 {
			middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
				{Field: "principal", Message: "principal must be non-negative"},
			}))
			return
		}
		principalOverride = &pVal
	}

	req := inbound.ProjectionRequest{
		UserId:              userId,
		MonthlyContribution: contribution,
		AnnualYieldPct:      yieldPct,
		Years:               years,
		Milestones:          milestones,
		PrincipalOverride:   principalOverride,
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
		MonthlyContributionUsd: result.MonthlyContribution.Float64(),
		AnnualYieldPct:         annualYieldFloat,
		Years:                  result.Years,
		Series:                 seriesRes,
		Milestones:             milestonesRes,
	}

	w.Header().Set("Cache-Control", "private, max-age=30")
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

	q := r.URL.Query()
	var from *time.Time
	if fromStr := q.Get("from"); fromStr != "" {
		t, err := time.Parse("2006-01-02", fromStr)
		if err == nil {
			utc := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			from = &utc
		}
	}

	var to *time.Time
	if toStr := q.Get("to"); toStr != "" {
		t, err := time.Parse("2006-01-02", toStr)
		if err == nil {
			utc := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.UTC)
			to = &utc
		}
	}

	snapshots, err := h.snapshotUseCase.GetSnapshots(r.Context(), userId, from, to)
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
		res[i] = dto.SnapshotResponse{
			Id:                    s.Snapshot.Id.String(),
			CapturedAt:            s.Snapshot.CapturedAt,
			TotalValueUsd:         s.Snapshot.TotalValue.Float64(),
			ChangePctFromPrevious: changePct,
		}
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

	res := dto.SnapshotResponse{
		Id:            snapshot.Id.String(),
		CapturedAt:    snapshot.CapturedAt,
		TotalValueUsd: snapshot.TotalValue.Float64(),
	}

	w.Header().Set("Location", "/api/v1/wealth/snapshots/"+snapshot.Id.String())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}

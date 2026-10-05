package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/go-chi/chi/v5"
)

// The milestones of an estimate that doesn't name its own.
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

// GetEstimate is GET /wealth/estimate: contribution and years are required; yieldPct (without it, the
// portfolio's expected return), milestones, inflationPct and contributionGrowthPct are optional. Every
// problem with them is reported at once.
func (h *WealthHandler) GetEstimate(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	q := r.URL.Query()
	var valErrors []errors.ValidationError
	fail := func(field, message string) {
		valErrors = append(valErrors, errors.ValidationError{Field: field, Message: message})
	}
	// number reads a parameter in [min, max]: required ones must be there, optional ones take fallback.
	number := func(field string, required bool, min, max, fallback float64, message string) float64 {
		raw := q.Get(field)
		if raw == "" {
			if required {
				fail(field, field+" is required")
			}
			return fallback
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || !inRange(v, min, max) {
			fail(field, message)
			return fallback
		}
		return v
	}

	contribution := number("contribution", true, 0, maxMonthlyContributionUsd, 0,
		"contribution must be between 0 and 1000000000")
	var yieldPct *float64
	if q.Get("yieldPct") != "" {
		v := number("yieldPct", false, -100, 100, 0, model.ErrYieldOutOfRange.Error())
		yieldPct = &v
	}
	years := 1
	if yearsStr := q.Get("years"); yearsStr == "" {
		fail("years", "years is required")
	} else if v, err := strconv.Atoi(yearsStr); err != nil || v < 1 || v > model.MaxProjectionYears {
		fail("years", "years must be between 1 and 50")
	} else {
		years = v
	}
	milestones := estimateMilestones
	if q.Has("milestones") {
		milestones = parseMilestones(q.Get("milestones"), fail)
	}
	inflationPct := number("inflationPct", false, 0, 50, 0, model.ErrInflationOutOfRange.Error())
	contributionGrowthPct := number("contributionGrowthPct", false, 0, 50, 0, model.ErrContributionGrowthOutOfRange.Error())
	if len(valErrors) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(valErrors))
		return
	}

	req := inbound.ProjectionRequest{
		UserId:                userId,
		MonthlyContribution:   contribution,
		AnnualYieldPct:        yieldPct,
		Years:                 years,
		Milestones:            milestones,
		InflationPct:          inflationPct,
		ContributionGrowthPct: contributionGrowthPct,
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
			RealFutureValueUsd:  pt.RealFutureValue.Float64(),
			RealNetWorthUsd:     pt.RealNetWorth.Float64(),
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

	response := dto.ProjectionResponse{
		PrincipalUsd:           result.Principal.Float64(),
		DebtsUsd:               result.Debts.Float64(),
		MonthlyContributionUsd: result.MonthlyContribution.Float64(),
		AnnualYieldPct:         result.AnnualYieldPct.InexactFloat64(),
		YieldSource:            string(result.YieldSource),
		PortfolioYieldPct:      optionalFloat(result.PortfolioYieldPct),
		InflationPct:           result.InflationPct.InexactFloat64(),
		ContributionGrowthPct:  result.ContributionGrowthPct.InexactFloat64(),
		Years:                  result.Years,
		Series:                 seriesRes,
		Milestones:             milestonesRes,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

// milestonesProblem is what's wrong with a milestones parameter.
var milestonesProblem = fmt.Sprintf("milestones must be up to %d comma-separated amounts between 0 and %s",
	model.MaxProjectionMilestones, model.MaxMilestoneUsd.String())

// parseMilestones reads "150000,250000" (empty: none).
func parseMilestones(raw string, fail func(field, message string)) []float64 {
	if strings.TrimSpace(raw) == "" {
		return []float64{}
	}
	parts := strings.Split(raw, ",")
	if len(parts) > model.MaxProjectionMilestones {
		fail("milestones", milestonesProblem)
		return nil
	}
	amounts := make([]float64, len(parts))
	for i, part := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			fail("milestones", milestonesProblem)
			return nil
		}
		if _, err := model.NewMilestone(v); err != nil {
			fail("milestones", milestonesProblem)
			return nil
		}
		amounts[i] = v
	}
	return amounts
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

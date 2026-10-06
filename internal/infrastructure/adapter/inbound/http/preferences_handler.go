package http

import (
	"encoding/json"
	"net/http"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
)

type PreferencesHandler struct {
	preferencesUseCase inbound.PreferencesUseCase
}

func NewPreferencesHandler(preferencesUseCase inbound.PreferencesUseCase) *PreferencesHandler {
	return &PreferencesHandler{preferencesUseCase: preferencesUseCase}
}

func toPreferencesDocument(p model.Preferences) dto.PreferencesDocument {
	e := p.Estimate
	milestones := make([]float64, len(e.Milestones))
	for i, m := range e.Milestones {
		milestones[i] = m.Float64()
	}
	return dto.PreferencesDocument{
		Estimate: dto.EstimatePreferencesDocument{
			ContributionUsd:       e.Contribution.Float64(),
			Years:                 e.Years,
			YieldMode:             string(e.YieldMode),
			CustomYieldPct:        e.CustomYieldPct.InexactFloat64(),
			MilestonesUsd:         milestones,
			InflationPct:          e.InflationPct.InexactFloat64(),
			ContributionGrowthPct: e.ContributionGrowthPct.InexactFloat64(),
		},
		AutoSnapshot:  string(p.AutoSnapshot),
		DefaultView:   string(p.DefaultView),
		HistoryPeriod: string(p.HistoryPeriod),
	}
}

func writePreferences(w http.ResponseWriter, p model.Preferences) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toPreferencesDocument(p))
}

// GetPreferences is GET /preferences: what the user saved, or the defaults.
func (h *PreferencesHandler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	preferences, err := h.preferencesUseCase.GetPreferences(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	writePreferences(w, preferences)
}

// ReplacePreferences is PUT /preferences: the whole document. What it leaves out takes its default and what's
// unknown is ignored; every value out of range is reported at once.
func (h *PreferencesHandler) ReplacePreferences(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	req := toPreferencesDocument(model.DefaultPreferences())
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	e := req.Estimate
	var valErrors []errors.ValidationError
	check := func(field string, ok bool, problem error) {
		if !ok {
			valErrors = append(valErrors, errors.ValidationError{Field: field, Message: problem.Error()})
		}
	}
	check("estimate.contributionUsd", inRange(e.ContributionUsd, 0, maxMonthlyContributionUsd), model.ErrContributionOutOfRange)
	check("estimate.years", e.Years >= 1 && e.Years <= model.MaxProjectionYears, model.ErrYearsOutOfRange)
	_, modeErr := model.ParseYieldMode(e.YieldMode)
	check("estimate.yieldMode", modeErr == nil, model.ErrUnknownYieldMode)
	check("estimate.customYieldPct", inRange(e.CustomYieldPct, -100, 100), model.ErrCustomYieldOutOfRange)
	check("estimate.milestonesUsd", len(e.MilestonesUsd) <= model.MaxProjectionMilestones, model.ErrTooManyMilestones)
	for _, m := range e.MilestonesUsd {
		if _, err := model.NewMilestone(m); err != nil {
			check("estimate.milestonesUsd", false, model.ErrMilestoneOutOfRange)
			break
		}
	}
	check("estimate.inflationPct", inRange(e.InflationPct, 0, 50), model.ErrInflationOutOfRange)
	check("estimate.contributionGrowthPct", inRange(e.ContributionGrowthPct, 0, 50), model.ErrContributionGrowthOutOfRange)
	_, autoErr := model.ParseAutoSnapshot(req.AutoSnapshot)
	check("autoSnapshot", autoErr == nil, model.ErrUnknownAutoSnapshot)
	_, viewErr := model.ParseStartView(req.DefaultView)
	check("defaultView", viewErr == nil, model.ErrUnknownStartView)
	_, periodErr := model.ParseHistoryPeriod(req.HistoryPeriod)
	check("historyPeriod", periodErr == nil, model.ErrUnknownHistoryPeriod)
	if len(valErrors) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(valErrors))
		return
	}

	saved, err := h.preferencesUseCase.ReplacePreferences(r.Context(), userId, inbound.PreferencesCommand{
		Estimate: inbound.EstimatePreferencesCommand{
			ContributionUsd:       e.ContributionUsd,
			Years:                 e.Years,
			YieldMode:             e.YieldMode,
			CustomYieldPct:        e.CustomYieldPct,
			MilestonesUsd:         e.MilestonesUsd,
			InflationPct:          e.InflationPct,
			ContributionGrowthPct: e.ContributionGrowthPct,
		},
		AutoSnapshot:  req.AutoSnapshot,
		DefaultView:   req.DefaultView,
		HistoryPeriod: req.HistoryPeriod,
	})
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	writePreferences(w, saved)
}

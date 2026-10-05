package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/errors"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
)

// Upper bound for a single holding: rejects absurd input (e.g. 1e30) at the API boundary.
const maxHoldingValueUsd = 1e15

type HoldingHandler struct {
	holdingUseCase inbound.HoldingUseCase
}

func NewHoldingHandler(holdingUseCase inbound.HoldingUseCase) *HoldingHandler {
	return &HoldingHandler{
		holdingUseCase: holdingUseCase,
	}
}

func (h *HoldingHandler) GetAllHoldings(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	holdings, err := h.holdingUseCase.GetAllHoldings(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	res := make([]dto.HoldingResponse, len(holdings))
	for i, item := range holdings {
		res[i] = toHoldingResponse(item)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *HoldingHandler) CreateHolding(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	var req dto.CreateHoldingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	var valErrors []errors.ValidationError
	if strings.TrimSpace(req.Name) == "" {
		valErrors = append(valErrors, errors.ValidationError{Field: "name", Message: "Name is required"})
	}
	if strings.TrimSpace(req.AssetClass) == "" {
		valErrors = append(valErrors, errors.ValidationError{Field: "assetClass", Message: "Asset class is required"})
	}
	if strings.TrimSpace(req.Platform) == "" {
		valErrors = append(valErrors, errors.ValidationError{Field: "platform", Message: "Platform is required"})
	}
	if req.ValueUsd < 0 {
		valErrors = append(valErrors, errors.ValidationError{Field: "valueUsd", Message: "Value must not be negative"})
	} else if req.ValueUsd > maxHoldingValueUsd {
		valErrors = append(valErrors, errors.ValidationError{Field: "valueUsd", Message: "Value is too large"})
	}
	if req.ExpectedReturnPct != nil && !validReturn(*req.ExpectedReturnPct) {
		valErrors = append(valErrors, errors.ValidationError{Field: "expectedReturnPct", Message: model.ErrExpectedReturnOutOfRange.Error()})
	}

	if len(valErrors) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(valErrors))
		return
	}

	cmd := inbound.CreateHoldingCommand{
		UserId:            userId,
		Name:              req.Name,
		AssetClass:        req.AssetClass,
		Platform:          req.Platform,
		ValueUsd:          req.ValueUsd,
		ExpectedReturnPct: req.ExpectedReturnPct,
	}

	created, err := h.holdingUseCase.CreateHolding(r.Context(), cmd)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/holdings/"+created.Id.String())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toHoldingResponse(created))
}

// UpdateHolding is PATCH /holdings/{id}: the fields sent change, with the checks and messages of
// CreateHolding. All four are required on a holding, so null is a validation error, not a way to clear one.
func (h *HoldingHandler) UpdateHolding(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	idStr := chi.URLParam(r, "id")
	holdingId, err := model.ParseHoldingId(idStr)
	if err != nil {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("Holding "+idStr+" not found"))
		return
	}

	var req dto.UpdateHoldingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	var valErrors []errors.ValidationError
	text := func(field, label string, sent dto.Optional[string]) *string {
		if !sent.Set {
			return nil
		}
		if sent.Null || strings.TrimSpace(sent.Value) == "" {
			valErrors = append(valErrors, errors.ValidationError{Field: field, Message: label + " is required"})
			return nil
		}
		return &sent.Value
	}
	cmd := inbound.UpdateHoldingCommand{
		UserId:     userId,
		Id:         holdingId,
		Name:       text("name", "Name", req.Name),
		AssetClass: text("assetClass", "Asset class", req.AssetClass),
		Platform:   text("platform", "Platform", req.Platform),
	}
	cmd.ValueChangeReason, cmd.Note = req.ValueChangeReason, req.Note
	if req.OccurredAt != "" {
		at, ok := parseWhen(req.OccurredAt, noon)
		if !ok {
			valErrors = append(valErrors, errors.ValidationError{Field: "occurredAt", Message: "occurredAt must be a date (YYYY-MM-DD) or a date and time (RFC 3339)"})
		}
		cmd.OccurredAt = &at
	}
	if sent := req.ValueUsd; sent.Set {
		switch {
		case sent.Null:
			valErrors = append(valErrors, errors.ValidationError{Field: "valueUsd", Message: "Value is required"})
		case sent.Value < 0:
			valErrors = append(valErrors, errors.ValidationError{Field: "valueUsd", Message: "Value must not be negative"})
		case sent.Value > maxHoldingValueUsd:
			valErrors = append(valErrors, errors.ValidationError{Field: "valueUsd", Message: "Value is too large"})
		default:
			cmd.ValueUsd = &sent.Value
		}
	}
	if sent := req.ExpectedReturnPct; sent.Set {
		if sent.Null {
			cmd.ExpectedReturnPct = inbound.Change[float64]{Set: true}
		} else if validReturn(sent.Value) {
			cmd.ExpectedReturnPct = inbound.Change[float64]{Set: true, Value: &sent.Value}
		} else {
			valErrors = append(valErrors, errors.ValidationError{Field: "expectedReturnPct", Message: model.ErrExpectedReturnOutOfRange.Error()})
		}
	}
	if len(valErrors) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(valErrors))
		return
	}

	updated, err := h.holdingUseCase.UpdateHolding(r.Context(), cmd)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toHoldingResponse(updated))
}

func (h *HoldingHandler) DeleteHolding(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	idStr := chi.URLParam(r, "id")
	holdingId, err := model.ParseHoldingId(idStr)
	if err != nil {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("Holding "+idStr+" not found"))
		return
	}

	if err := h.holdingUseCase.DeleteHolding(r.Context(), userId, holdingId); err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// validReturn is whether a yearly return is one the API takes (-100 to 100; it keeps 2 decimals).
func validReturn(v float64) bool {
	_, err := model.NewExpectedReturnPct(v)
	return err == nil
}

// maxExpectedReturnItems is how many returns one PUT /holdings/expected-returns sets: every holding a user
// can have.
const maxExpectedReturnItems = model.MaxHoldingsPerUser

// SetExpectedReturns is PUT /holdings/expected-returns: many holdings' yearly returns at once (null clears
// one), all or none. Every problem with the items is reported at once; an id that isn't a holding's is 404,
// as anywhere in a body.
func (h *HoldingHandler) SetExpectedReturns(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	var req dto.ExpectedReturnsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}
	if req.Items == nil {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "items", Message: "items is required"},
		}))
		return
	}
	if len(*req.Items) > maxExpectedReturnItems {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "items", Message: fmt.Sprintf("items can't have more than %d entries", maxExpectedReturnItems)},
		}))
		return
	}

	var valErrors []errors.ValidationError
	items := make([]inbound.ExpectedReturnItem, 0, len(*req.Items))
	seen := make(map[string]bool, len(*req.Items))
	var unknown string
	for i, item := range *req.Items {
		field := fmt.Sprintf("items[%d]", i)
		switch {
		case strings.TrimSpace(item.HoldingId) == "":
			valErrors = append(valErrors, errors.ValidationError{Field: field + ".holdingId", Message: field + ".holdingId is required"})
		case seen[item.HoldingId]:
			valErrors = append(valErrors, errors.ValidationError{Field: field + ".holdingId", Message: field + ".holdingId appears more than once"})
		}
		seen[item.HoldingId] = true
		sent := item.ExpectedReturnPct
		switch {
		case !sent.Set:
			valErrors = append(valErrors, errors.ValidationError{Field: field + ".expectedReturnPct",
				Message: field + ".expectedReturnPct is required (null clears it)"})
		case !sent.Null && !validReturn(sent.Value):
			valErrors = append(valErrors, errors.ValidationError{Field: field + ".expectedReturnPct",
				Message: field + "." + model.ErrExpectedReturnOutOfRange.Error()})
		}
		id, err := model.ParseHoldingId(item.HoldingId)
		if err != nil {
			if unknown == "" && strings.TrimSpace(item.HoldingId) != "" {
				unknown = item.HoldingId
			}
			continue
		}
		next := inbound.ExpectedReturnItem{HoldingId: id}
		if sent.Set && !sent.Null {
			v := sent.Value
			next.ExpectedReturnPct = &v
		}
		items = append(items, next)
	}
	if len(valErrors) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(valErrors))
		return
	}
	if unknown != "" {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("Holding "+unknown+" not found"))
		return
	}

	updated, err := h.holdingUseCase.SetExpectedReturns(r.Context(), userId, items)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	res := make([]dto.HoldingResponse, len(updated))
	for i, item := range updated {
		res[i] = toHoldingResponse(item)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func optionalFloat(d *decimal.Decimal) *float64 {
	if d == nil {
		return nil
	}
	f := d.InexactFloat64()
	return &f
}

func toHoldingResponse(h model.Holding) dto.HoldingResponse {
	return dto.HoldingResponse{
		Id:                 h.Id.String(),
		Name:               h.Name,
		AssetClass:         h.AssetClass.Value(),
		Platform:           h.Platform.Value(),
		ValueUsd:           h.Value.Float64(),
		ExpectedReturnPct:  optionalFloat(h.ExpectedReturnPct),
		EffectiveReturnPct: optionalFloat(h.EffectiveReturnPct()),
		CreatedAt:          h.CreatedAt,
		UpdatedAt:          h.UpdatedAt,
	}
}

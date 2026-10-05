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

	if len(valErrors) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(valErrors))
		return
	}

	cmd := inbound.CreateHoldingCommand{
		UserId:     userId,
		Name:       req.Name,
		AssetClass: req.AssetClass,
		Platform:   req.Platform,
		ValueUsd:   req.ValueUsd,
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

func toHoldingResponse(h model.Holding) dto.HoldingResponse {
	return dto.HoldingResponse{
		Id:         h.Id.String(),
		Name:       h.Name,
		AssetClass: h.AssetClass.Value(),
		Platform:   h.Platform.Value(),
		ValueUsd:   h.Value.Float64(),
		CreatedAt:  h.CreatedAt,
		UpdatedAt:  h.UpdatedAt,
	}
}

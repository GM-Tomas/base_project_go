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

	var assetClass *model.AssetClass
	if acParam := strings.TrimSpace(r.URL.Query().Get("assetClass")); acParam != "" {
		ac, err := model.NewAssetClass(acParam)
		if err != nil {
			middleware.HandleError(w, r, err)
			return
		}
		assetClass = &ac
	}

	var platform *model.PlatformName
	if platParam := strings.TrimSpace(r.URL.Query().Get("platform")); platParam != "" {
		pn, err := model.NewPlatformName(platParam)
		if err != nil {
			middleware.HandleError(w, r, err)
			return
		}
		platform = &pn
	}

	holdings, err := h.holdingUseCase.GetAllHoldings(r.Context(), userId, assetClass, platform)
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

func (h *HoldingHandler) GetHoldingById(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	idStr := chi.URLParam(r, "id")
	holdingId, err := model.ParseHoldingId(idStr)
	if err != nil {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("No se encontró el holding con ID: "+idStr))
		return
	}

	holding, err := h.holdingUseCase.GetHoldingById(r.Context(), userId, holdingId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toHoldingResponse(holding))
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
		valErrors = append(valErrors, errors.ValidationError{Field: "name", Message: "El nombre del activo no puede estar vacío"})
	}
	if strings.TrimSpace(req.AssetClass) == "" {
		valErrors = append(valErrors, errors.ValidationError{Field: "assetClass", Message: "La clase de activo es obligatoria"})
	}
	if strings.TrimSpace(req.Platform) == "" {
		valErrors = append(valErrors, errors.ValidationError{Field: "platform", Message: "La plataforma es obligatoria"})
	}
	if req.ValueUsd < 0 {
		valErrors = append(valErrors, errors.ValidationError{Field: "valueUsd", Message: "El valor no puede ser negativo"})
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

func (h *HoldingHandler) UpdateHolding(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	idStr := chi.URLParam(r, "id")
	holdingId, err := model.ParseHoldingId(idStr)
	if err != nil {
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("No se encontró el holding con ID: "+idStr))
		return
	}

	var req dto.UpdateHoldingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	cmd := inbound.PatchHoldingCommand{
		UserId:     userId,
		Id:         holdingId,
		Name:       req.Name,
		AssetClass: req.AssetClass,
		Platform:   req.Platform,
		ValueUsd:   req.ValueUsd,
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
		middleware.HandleError(w, r, errors.NewResourceNotFoundError("No se encontró el holding con ID: "+idStr))
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

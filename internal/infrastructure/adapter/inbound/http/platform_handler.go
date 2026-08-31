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

type PlatformHandler struct {
	platformUseCase inbound.PlatformUseCase
}

func NewPlatformHandler(platformUseCase inbound.PlatformUseCase) *PlatformHandler {
	return &PlatformHandler{
		platformUseCase: platformUseCase,
	}
}

func (h *PlatformHandler) GetAllPlatforms(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	platforms, err := h.platformUseCase.GetAllPlatforms(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	res := make([]dto.PlatformResponse, len(platforms))
	for i, p := range platforms {
		res[i] = toPlatformResponse(p)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

func (h *PlatformHandler) CreatePlatform(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	var req dto.CreatePlatformRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		middleware.HandleError(w, r, errors.NewValidationErrors([]errors.ValidationError{
			{Field: "name", Message: "El nombre de la plataforma no puede estar vacío"},
		}))
		return
	}

	pType := req.Type
	if pType == "" {
		pType = "Other"
	}

	created, err := h.platformUseCase.CreatePlatform(r.Context(), inbound.CreatePlatformCommand{
		UserId: userId,
		Name:   req.Name,
		Type:   pType,
	})
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toPlatformResponse(created))
}

func (h *PlatformHandler) PatchPlatform(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	name := chi.URLParam(r, "name")

	var req dto.PatchPlatformRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	updated, err := h.platformUseCase.PatchPlatform(r.Context(), inbound.PatchPlatformCommand{
		UserId:  userId,
		Name:    name,
		NewName: req.Name,
		NewType: req.Type,
	})
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toPlatformResponse(updated))
}

func (h *PlatformHandler) DeletePlatform(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	name := chi.URLParam(r, "name")

	if err := h.platformUseCase.DeletePlatform(r.Context(), userId, name); err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toPlatformResponse(p model.Platform) dto.PlatformResponse {
	return dto.PlatformResponse{
		Name:      p.Name.Value(),
		Type:      p.Type.Value(),
		CreatedAt: p.CreatedAt,
	}
}

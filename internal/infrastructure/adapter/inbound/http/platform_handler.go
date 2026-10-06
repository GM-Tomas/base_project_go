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
	writeJSON(w, http.StatusOK, res)
}

// UpdatePlatform is PATCH /platforms/{id}: the fields sent change (null sets type, avatarText and color back
// to their defaults). A new name renames the platform on all its holdings, or, with mergeIfExists, merges it
// into another the user has.
func (h *PlatformHandler) UpdatePlatform(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}
	var req dto.UpdatePlatformRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteProblem(w, r, http.StatusBadRequest, "bad-request", "Bad Request", "Malformed JSON body", nil)
		return
	}

	var problems []errors.ValidationError
	fail := func(field, message string) {
		problems = append(problems, errors.ValidationError{Field: field, Message: message})
	}
	cmd := inbound.UpdatePlatformCommand{UserId: userId, Id: chi.URLParam(r, "id"), MergeIfExists: req.MergeIfExists}
	if sent := req.Name; sent.Set {
		if strings.TrimSpace(sent.Value) == "" { // null reads as blank: required
			fail("name", "Name is required")
		} else if _, err := model.NewPlatformName(sent.Value); err != nil {
			fail("name", fmt.Sprintf("Name must be at most %d characters", model.MaxPlatformNameLength))
		}
		cmd.Name = &sent.Value
	}
	cmd.Type = change(req.Type, func(v string) {
		if _, err := model.NewPlatformType(v); err != nil && strings.TrimSpace(v) != "" { // blank: the default
			fail("type", fmt.Sprintf("type must be at most %d characters", model.MaxPlatformTypeLength))
		}
	})
	cmd.AvatarText = change(req.AvatarText, func(v string) {
		if _, err := model.NewAvatarText(v); err != nil {
			fail("avatarText", err.Error())
		}
	})
	cmd.Color = change(req.Color, func(v string) {
		if _, err := model.NewColor(v); err != nil {
			fail("color", err.Error())
		}
	})
	if len(problems) > 0 {
		middleware.HandleError(w, r, errors.NewValidationErrors(problems))
		return
	}

	updated, err := h.platformUseCase.UpdatePlatform(r.Context(), cmd)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toPlatformResponse(updated))
}

func toPlatformResponse(p model.Platform) dto.PlatformResponse {
	return dto.PlatformResponse{
		Id:            model.PlatformId(p.Key),
		Name:          p.Name.Value(),
		Type:          p.Type.Value(),
		AvatarText:    p.AvatarText,
		Color:         dto.ColorOf(p.Color),
		HoldingsCount: p.Count,
		ValueUsd:      p.Value.Float64(),
		CreatedAt:     p.CreatedAt,
	}
}

package http

import (
	"encoding/json"
	"net/http"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
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

func toPlatformResponse(p model.Platform) dto.PlatformResponse {
	return dto.PlatformResponse{
		Name:      p.Name.Value(),
		Type:      p.Type.Value(),
		CreatedAt: p.CreatedAt,
	}
}

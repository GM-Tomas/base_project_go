package http

import (
	"encoding/json"
	"net/http"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/infrastructure/adapter/inbound/http/middleware"
)

type AssetClassHandler struct {
	assetClassUseCase inbound.AssetClassUseCase
}

func NewAssetClassHandler(assetClassUseCase inbound.AssetClassUseCase) *AssetClassHandler {
	return &AssetClassHandler{
		assetClassUseCase: assetClassUseCase,
	}
}

func (h *AssetClassHandler) GetAvailableAssetClasses(w http.ResponseWriter, r *http.Request) {
	userId, err := middleware.GetUserFromContext(r.Context())
	if err != nil {
		middleware.WriteUnauthorized(w, r, "")
		return
	}

	res, err := h.assetClassUseCase.GetAvailableAssetClasses(r.Context(), userId)
	if err != nil {
		middleware.HandleError(w, r, err)
		return
	}

	response := dto.AvailableAssetClassesResponse{
		Defaults: res.Defaults,
		InUse:    res.InUse,
		All:      res.All,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

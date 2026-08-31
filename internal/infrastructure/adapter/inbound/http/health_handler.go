package http

import (
	"encoding/json"
	"net/http"
	"time"
)

type HealthStatusResponse struct {
	Status      string    `json:"status"`
	Service     string    `json:"service"`
	Timestamp   time.Time `json:"timestamp"`
	Environment string    `json:"environment"`
	Version     string    `json:"version"`
}

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) CheckHealth(w http.ResponseWriter, r *http.Request) {
	res := HealthStatusResponse{
		Status:      "UP",
		Service:     "base-wealth-backend",
		Timestamp:   time.Now().UTC(),
		Environment: "production-ready",
		Version:     "1.0.0",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(res)
}

package dto

import "time"

type CreatePlatformRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type PatchPlatformRequest struct {
	Name *string `json:"name,omitempty"`
	Type *string `json:"type,omitempty"`
}

type PlatformResponse struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
}

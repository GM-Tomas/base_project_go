package dto

import "time"

// PlatformResponse is one of the user's platforms: avatarText and color are its thumbnail's (null: the
// default, its initial and a color from its name), holdingsCount and valueUsd what's on it.
type PlatformResponse struct {
	Id            string    `json:"id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	AvatarText    *string   `json:"avatarText"`
	Color         *string   `json:"color"`
	HoldingsCount int       `json:"holdingsCount"`
	ValueUsd      float64   `json:"valueUsd"`
	CreatedAt     time.Time `json:"createdAt"`
}

// UpdatePlatformRequest is a JSON merge patch: null sets type, avatarText and color back to their defaults.
type UpdatePlatformRequest struct {
	Name          Optional[string] `json:"name"`
	Type          Optional[string] `json:"type"`
	AvatarText    Optional[string] `json:"avatarText"`
	Color         Optional[string] `json:"color"`
	MergeIfExists bool             `json:"mergeIfExists"`
}

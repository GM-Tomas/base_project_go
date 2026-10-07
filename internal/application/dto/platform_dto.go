package dto

import "time"

// PlatformResponse is one of the user's platforms: avatarText, color and textColor are its thumbnail's (null:
// the default, its initial, a color from its name and its letters in that color), holdingsCount and valueUsd
// what's on it.
type PlatformResponse struct {
	Id            string    `json:"id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	AvatarText    *string   `json:"avatarText"`
	Color         *string   `json:"color"`
	TextColor     *string   `json:"textColor"`
	HoldingsCount int       `json:"holdingsCount"`
	ValueUsd      float64   `json:"valueUsd"`
	CreatedAt     time.Time `json:"createdAt"`
}

// UpdatePlatformRequest is a JSON merge patch: null sets type, avatarText, color and textColor back to their
// defaults.
type UpdatePlatformRequest struct {
	Name          Optional[string] `json:"name"`
	Type          Optional[string] `json:"type"`
	AvatarText    Optional[string] `json:"avatarText"`
	Color         Optional[string] `json:"color"`
	TextColor     Optional[string] `json:"textColor"`
	MergeIfExists bool             `json:"mergeIfExists"`
}

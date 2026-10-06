package dto

import "github.com/GM-Tomas/base_project_go/internal/domain/model"

// AvailableAssetClassesResponse: defaults are the classes every account starts with, inUse those the
// user's holdings have, all the ones the user has (in the order to offer them), classes the same with how
// each is set up.
type AvailableAssetClassesResponse struct {
	Defaults []string             `json:"defaults"`
	InUse    []string             `json:"inUse"`
	All      []string             `json:"all"`
	Classes  []AssetClassResponse `json:"classes"`
}

// AssetClassResponse is one of the user's classes: color null is its default one, expectedReturnPct the
// return its holdings without one count with (null: none).
type AssetClassResponse struct {
	Id                string   `json:"id"`
	Name              string   `json:"name"`
	Color             *string  `json:"color"`
	Liquid            bool     `json:"liquid"`
	ExpectedReturnPct *float64 `json:"expectedReturnPct"`
	IsDefault         bool     `json:"isDefault"`
	HoldingsCount     int      `json:"holdingsCount"`
	ValueUsd          float64  `json:"valueUsd"`
}

type CreateAssetClassRequest struct {
	Name              string   `json:"name"`
	Color             *string  `json:"color"`
	Liquid            *bool    `json:"liquid"`
	ExpectedReturnPct *float64 `json:"expectedReturnPct"`
}

// UpdateAssetClassRequest is a JSON merge patch: null clears color and expectedReturnPct, and sets liquid
// back to its default.
type UpdateAssetClassRequest struct {
	Name              Optional[string]  `json:"name"`
	Color             Optional[string]  `json:"color"`
	Liquid            Optional[bool]    `json:"liquid"`
	ExpectedReturnPct Optional[float64] `json:"expectedReturnPct"`
	MergeIfExists     bool              `json:"mergeIfExists"`
}

// ColorOf is a color as the API writes it (null: the default one).
func ColorOf(c *model.Color) *string {
	if c == nil {
		return nil
	}
	v := c.Value()
	return &v
}

package model

import (
	"strings"
	"time"
)

const MaxHoldingNameLength = 120

// Holding represents an asset position owned by a user.
type Holding struct {
	Id         HoldingId
	UserId     UserId
	Name       string
	AssetClass AssetClass
	Platform   PlatformName
	Value      Money
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// CreateHolding creates a new Holding with a new HoldingId and current timestamps.
func CreateHolding(
	userId UserId,
	name string,
	assetClass AssetClass,
	platform PlatformName,
	value Money,
	now time.Time,
) (Holding, error) {
	normName, err := NormalizeLabel(name, MaxHoldingNameLength, "Holding name")
	if err != nil {
		return Holding{}, err
	}
	return Holding{
		Id:         NewHoldingId(),
		UserId:     userId,
		Name:       normName,
		AssetClass: assetClass,
		Platform:   platform,
		Value:      value,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// Patch updates the holding with optional fields, setting UpdatedAt to now.
func (h Holding) Patch(
	name *string,
	assetClass *AssetClass,
	platform *PlatformName,
	value *Money,
	now time.Time,
) (Holding, error) {
	updatedName := h.Name
	if name != nil {
		norm, err := NormalizeLabel(*name, MaxHoldingNameLength, "Holding name")
		if err != nil {
			return Holding{}, err
		}
		updatedName = norm
	}

	updatedAssetClass := h.AssetClass
	if assetClass != nil {
		updatedAssetClass = *assetClass
	}

	updatedPlatform := h.Platform
	if platform != nil {
		updatedPlatform = *platform
	}

	updatedValue := h.Value
	if value != nil {
		updatedValue = *value
	}

	return Holding{
		Id:         h.Id,
		UserId:     h.UserId,
		Name:       strings.TrimSpace(updatedName),
		AssetClass: updatedAssetClass,
		Platform:   updatedPlatform,
		Value:      updatedValue,
		CreatedAt:  h.CreatedAt,
		UpdatedAt:  now,
	}, nil
}

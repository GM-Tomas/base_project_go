package model

import (
	"time"
)

// Platform represents a financial platform owned by a user.
type Platform struct {
	UserId    UserId
	Name      PlatformName
	Type      PlatformType
	CreatedAt time.Time
}

func NewPlatform(userId UserId, name PlatformName, pType PlatformType, createdAt time.Time) Platform {
	return Platform{
		UserId:    userId,
		Name:      name,
		Type:      pType,
		CreatedAt: createdAt,
	}
}

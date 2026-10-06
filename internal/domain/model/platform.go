package model

import (
	"time"
)

// Platform represents a financial platform owned by a user: a name their holdings use (see PlatformKey).
type Platform struct {
	UserId    UserId
	Name      PlatformName
	Type      PlatformType
	CreatedAt time.Time
	// Key is what its holdings' platform names have in common (see PlatformKey).
	Key string
	// Count and Value are its holdings with a readable amount, and what they're worth.
	Count int
	Value Money
	// AvatarText and Color are its thumbnail's, as the user set it; nil is the default.
	AvatarText *string
	Color      *Color
}

// WithSettings is the platform as the user set it up.
func (p Platform) WithSettings(s PlatformSettings) Platform {
	if s.Type != nil {
		p.Type = *s.Type
	}
	p.AvatarText, p.Color = s.AvatarText, s.Color
	return p
}

func NewPlatform(userId UserId, name PlatformName, pType PlatformType, createdAt time.Time) Platform {
	return Platform{
		UserId:    userId,
		Name:      name,
		Type:      pType,
		CreatedAt: createdAt,
	}
}

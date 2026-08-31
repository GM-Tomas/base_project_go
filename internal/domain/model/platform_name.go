package model

const MaxPlatformNameLength = 120

// PlatformName represents the natural key for a Platform (unique per user, case-insensitively).
type PlatformName struct {
	value string
}

func NewPlatformName(raw string) (PlatformName, error) {
	norm, err := NormalizeLabel(raw, MaxPlatformNameLength, "PlatformName")
	if err != nil {
		return PlatformName{}, err
	}
	return PlatformName{value: norm}, nil
}

func MustPlatformName(raw string) PlatformName {
	pn, err := NewPlatformName(raw)
	if err != nil {
		panic(err)
	}
	return pn
}

func (pn PlatformName) Value() string {
	return pn.value
}

func (pn PlatformName) String() string {
	return pn.value
}

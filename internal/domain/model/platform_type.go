package model

const (
	MaxPlatformTypeLength = 40
	DefaultPlatformType   = "Other"
)

var (
	PlatformTypeOther = PlatformType{value: DefaultPlatformType}
)

// PlatformType represents a platform category (e.g. Broker, Wallet, Exchange, Other).
type PlatformType struct {
	value string
}

func NewPlatformType(raw string) (PlatformType, error) {
	if raw == "" {
		return PlatformTypeOther, nil
	}
	norm, err := NormalizeLabel(raw, MaxPlatformTypeLength, "PlatformType")
	if err != nil {
		return PlatformType{}, err
	}
	return PlatformType{value: norm}, nil
}

func MustPlatformType(raw string) PlatformType {
	pt, err := NewPlatformType(raw)
	if err != nil {
		panic(err)
	}
	return pt
}

func (pt PlatformType) Value() string {
	return pt.value
}

func (pt PlatformType) String() string {
	return pt.value
}

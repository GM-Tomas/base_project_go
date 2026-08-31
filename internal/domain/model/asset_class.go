package model

const MaxAssetClassLength = 60

// AssetClass represents an asset category (free text, case-sensitive).
type AssetClass struct {
	value string
}

func NewAssetClass(raw string) (AssetClass, error) {
	norm, err := NormalizeLabel(raw, MaxAssetClassLength, "AssetClass")
	if err != nil {
		return AssetClass{}, err
	}
	return AssetClass{value: norm}, nil
}

func MustAssetClass(raw string) AssetClass {
	ac, err := NewAssetClass(raw)
	if err != nil {
		panic(err)
	}
	return ac
}

func (ac AssetClass) Value() string {
	return ac.value
}

func (ac AssetClass) String() string {
	return ac.value
}

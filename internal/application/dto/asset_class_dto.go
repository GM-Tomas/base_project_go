package dto

type AvailableAssetClassesResponse struct {
	Defaults []string `json:"defaults"`
	InUse    []string `json:"inUse"`
	All      []string `json:"all"`
}

package service

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/inbound"
	"github.com/GM-Tomas/base_project_go/internal/domain/port/outbound"
)

var DefaultAssetClasses = []string{
	"Cash",
	"Fixed Income",
	"Index Fund",
	"Equity",
	"Crypto",
}

type AssetClassService struct {
	holdingRepo        outbound.HoldingRepository
	defaultAssetClasses []string
}

func NewAssetClassService(
	holdingRepo outbound.HoldingRepository,
	defaultAssetClasses []string,
) *AssetClassService {
	if len(defaultAssetClasses) == 0 {
		defaultAssetClasses = DefaultAssetClasses
	}
	return &AssetClassService{
		holdingRepo:        holdingRepo,
		defaultAssetClasses: defaultAssetClasses,
	}
}

var _ inbound.AssetClassUseCase = (*AssetClassService)(nil)

func (s *AssetClassService) GetAvailableAssetClasses(
	ctx context.Context,
	userId model.UserId,
) (inbound.AvailableAssetClasses, error) {
	inUseClasses, err := s.holdingRepo.AssetClassesInUse(ctx, userId)
	if err != nil {
		return inbound.AvailableAssetClasses{}, err
	}

	inUse := make([]string, len(inUseClasses))
	for i, c := range inUseClasses {
		inUse[i] = c.Value()
	}

	allMap := make(map[string]struct{})
	all := make([]string, 0, len(s.defaultAssetClasses)+len(inUse))

	for _, d := range s.defaultAssetClasses {
		if _, exists := allMap[d]; !exists {
			allMap[d] = struct{}{}
			all = append(all, d)
		}
	}
	for _, u := range inUse {
		if _, exists := allMap[u]; !exists {
			allMap[u] = struct{}{}
			all = append(all, u)
		}
	}

	return inbound.AvailableAssetClasses{
		Defaults: s.defaultAssetClasses,
		InUse:    inUse,
		All:      all,
	}, nil
}

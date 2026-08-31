package inbound

import (
	"context"

	"github.com/GM-Tomas/base_project_go/internal/application/dto"
	"github.com/GM-Tomas/base_project_go/internal/domain/model"
)

type WealthUseCase interface {
	GetSummary(ctx context.Context, userId model.UserId) (dto.WealthSummaryResponse, error)
}

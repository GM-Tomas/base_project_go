package service_test

import (
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/GM-Tomas/base_project_go/internal/domain/service"
	"github.com/stretchr/testify/assert"
)

func TestLiquidityPolicy_UnknownClassIsIlliquid(t *testing.T) {
	cash := model.MustAssetClass("Cash")
	equity := model.MustAssetClass("Equity")
	policy := service.NewLiquidityPolicy([]model.AssetClass{cash, equity})

	breakdown := policy.Breakdown(map[string]model.Money{
		"Cash":        model.MustMoneyFromFloat(50.0),
		"Real Estate": model.MustMoneyFromFloat(50.0),
	})

	assert.Equal(t, "50.0", breakdown.LiquidPct.StringFixed(1))
	assert.Equal(t, "50.0", breakdown.IlliquidPct.StringFixed(1))
}

func TestLiquidityPolicy_AllLiquid(t *testing.T) {
	cash := model.MustAssetClass("Cash")
	equity := model.MustAssetClass("Equity")
	policy := service.NewLiquidityPolicy([]model.AssetClass{cash, equity})

	breakdown := policy.Breakdown(map[string]model.Money{
		"Cash":   model.MustMoneyFromFloat(100.0),
		"Equity": model.MustMoneyFromFloat(50.0),
	})

	assert.Equal(t, "100.0", breakdown.LiquidPct.StringFixed(1))
	assert.Equal(t, "0.0", breakdown.IlliquidPct.StringFixed(1))
}

func TestLiquidityPolicy_EmptyPortfolio(t *testing.T) {
	cash := model.MustAssetClass("Cash")
	equity := model.MustAssetClass("Equity")
	policy := service.NewLiquidityPolicy([]model.AssetClass{cash, equity})

	breakdown := policy.Breakdown(map[string]model.Money{})

	assert.Equal(t, "0.0", breakdown.LiquidPct.StringFixed(1))
	assert.Equal(t, "0.0", breakdown.IlliquidPct.StringFixed(1))
}

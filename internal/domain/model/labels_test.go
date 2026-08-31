package model_test

import (
	"strings"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLabels_NormalizesWhitespace(t *testing.T) {
	ac, err := model.NewAssetClass("  Fixed   Income  ")
	require.NoError(t, err)
	assert.Equal(t, "Fixed Income", ac.Value())
}

func TestLabels_RejectsBlank(t *testing.T) {
	_, err := model.NewAssetClass("   ")
	assert.Error(t, err)
}

func TestLabels_AssetClassIsCaseSensitive(t *testing.T) {
	ac1 := model.MustAssetClass("Crypto")
	ac2 := model.MustAssetClass("crypto")
	assert.NotEqual(t, ac1.Value(), ac2.Value())
}

func TestLabels_RejectsOverLength(t *testing.T) {
	longStr := strings.Repeat("x", 121)
	_, err := model.NewPlatformName(longStr)
	assert.Error(t, err)
}

func TestLabels_PlatformTypeDefault(t *testing.T) {
	assert.Equal(t, "Other", model.PlatformTypeOther.Value())
}

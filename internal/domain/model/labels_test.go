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

func TestLabels_NormalizesUnicodeForm(t *testing.T) {
	// "é" as one code point, or as "e" and a combining accent: the same label either way.
	ac, err := model.NewAssetClass("Cafe\u0301")
	require.NoError(t, err)
	assert.Equal(t, "Caf\u00e9", ac.Value())

	pn, err := model.NewPlatformName(strings.Repeat("e\u0301", model.MaxPlatformNameLength))
	require.NoError(t, err, "the length counts the label as shown, not how it was typed")
	assert.Equal(t, strings.Repeat("\u00e9", model.MaxPlatformNameLength), pn.Value())
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

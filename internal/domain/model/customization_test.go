package model_test

import (
	"strings"
	"testing"

	"github.com/GM-Tomas/base_project_go/internal/domain/model"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestColor(t *testing.T) {
	c, err := model.NewColor("#A1b2C3")
	require.NoError(t, err)
	assert.Equal(t, "#a1b2c3", c.Value())
	for _, bad := range []string{"", "a1b2c3", "#a1b2c", "#a1b2c3d", "#gggggg", " #a1b2c3", "red"} {
		_, err := model.NewColor(bad)
		assert.ErrorIs(t, err, model.ErrInvalidColor, bad)
	}
	assert.Panics(t, func() { model.MustColor("nope") })
}

func TestGraphemeCount(t *testing.T) {
	for text, want := range map[string]int{
		"":        0,
		"A":       1,
		"AB":      2,
		"e\u0301": 1, // e + combining acute
		"\u00e9":  1, // precomposed
		"🟡":       1,
		"👍🏽":      1, // skin tone
		"👨‍👩‍👧":   1, // joined family
		"🇦🇷":      1, // flag
		"🇦🇷🇧🇷":    2,
		"1️⃣":     1, // keycap
		"❤️":      1, // variation selector
		"🏴󠁧󠁢󠁳󠁣󠁴󠁿": 1, // tag sequence
		"B🟡":      2,
		"́":       1, // a lone mark is still something
		"‍":       1,
	} {
		assert.Equal(t, want, model.GraphemeCount(text), "%q", text)
	}
}

func TestAvatarText(t *testing.T) {
	for raw, want := range map[string]string{" B ": "B", "bn": "bn", "🟡": "🟡", "₿": "₿", "é": "é", "🇦🇷": "🇦🇷"} {
		got, err := model.NewAvatarText(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got)
	}
	for _, bad := range []string{"", "  ", "ABC", "🟡🟡🟡"} {
		_, err := model.NewAvatarText(bad)
		assert.ErrorIs(t, err, model.ErrInvalidAvatarText, bad)
	}
}

func TestAssetClassIds(t *testing.T) {
	class := model.MustAssetClass("Fixed Income")
	id := model.AssetClassId(class)
	assert.Equal(t, "Rml4ZWQgSW5jb21l", id)
	back, err := model.ParseAssetClassId(id)
	require.NoError(t, err)
	assert.Equal(t, class, back)

	// Only ids this API gives: base64url of a class as it's kept.
	for _, bad := range []string{"", "!!", "Rml4ZWQgSW5jb21l=", model.AssetClassId(model.AssetClass{}),
		"IENhc2gg" /* " Cash " */, "Q2FzaCAgQ2FzaA" /* "Cash  Cash" */} {
		_, err := model.ParseAssetClassId(bad)
		assert.ErrorIs(t, err, model.ErrUnknownId, bad)
	}
}

func TestPlatformKeysAndIds(t *testing.T) {
	assert.Equal(t, model.PlatformKey(model.MustPlatformName("BINANCE")), model.PlatformKey(model.MustPlatformName("binance")))
	assert.Equal(t, model.PlatformKey(model.MustPlatformName("Straße")), model.PlatformKey(model.MustPlatformName("STRASSE")))
	assert.Equal(t, model.PlatformKey(model.MustPlatformName("Café")), model.PlatformKey(model.MustPlatformName("Café")))

	key := model.PlatformKey(model.MustPlatformName("Mercado Pago"))
	back, err := model.ParsePlatformKey(model.PlatformId(key))
	require.NoError(t, err)
	assert.Equal(t, key, back)
	for _, bad := range []string{"", "%%%"} {
		_, err := model.ParsePlatformKey(bad)
		assert.ErrorIs(t, err, model.ErrUnknownId)
	}
}

func TestSortName(t *testing.T) {
	assert.Equal(t, model.SortName("álamo"), model.SortName("Alamo"))
	assert.Less(t, model.SortName("Álamo"), model.SortName("zurich"))
}

func TestClasses(t *testing.T) {
	cash, equity, art, gold := model.MustAssetClass("Cash"), model.MustAssetClass("Equity"), model.MustAssetClass("Art"), model.MustAssetClass("gold")
	defaults := model.ClassDefaults{Names: []model.AssetClass{cash, equity}, Liquid: []model.AssetClass{cash}}
	six, blue := decimal.NewFromInt(6), model.MustColor("#0000ff")
	classes := model.NewClasses(defaults, []model.AssetClassSettings{
		{Name: equity, Hidden: true},
		{Name: art, Liquid: ptr(true), ExpectedReturnPct: &six, Color: &blue},
		{Name: cash, Liquid: ptr(false)},
	})

	assert.Equal(t, []model.AssetClass{cash, art, gold}, classes.Visible([]model.AssetClass{gold}))
	assert.Equal(t, []model.AssetClass{cash, art, equity}, classes.Visible([]model.AssetClass{equity}), "a removed default in use is there, with the rest")
	assert.True(t, classes.IsDefault(equity))
	assert.False(t, classes.IsDefault(art))
	assert.False(t, classes.Liquid(cash), "as the user set it")
	assert.True(t, classes.Liquid(art))
	assert.False(t, classes.Liquid(gold))
	assert.Equal(t, &blue, classes.Color(art))
	assert.Nil(t, classes.Color(gold))
	s, ok := classes.Settings(art)
	assert.True(t, ok)
	assert.True(t, s.Customized())
	_, ok = classes.Settings(gold)
	assert.False(t, ok)
	assert.False(t, model.AssetClassSettings{Name: gold}.Customized())

	three := decimal.NewFromInt(3)
	returns := classes.WithClassReturns([]model.HoldingReturn{{Class: art}, {Class: art, Pct: &three}, {Class: gold}})
	assert.Equal(t, "6", returns[0].Pct.String())
	assert.Equal(t, "3", returns[1].Pct.String())
	assert.Nil(t, returns[2].Pct)
}

func TestPlatformWithSettings(t *testing.T) {
	p := model.Platform{Name: model.MustPlatformName("Nexo"), Type: model.MustPlatformType("Wallet")}
	assert.Equal(t, p, p.WithSettings(model.PlatformSettings{}))
	exchange, red := model.MustPlatformType("Exchange"), model.MustColor("#ff0000")
	text := "NX"
	set := p.WithSettings(model.PlatformSettings{Type: &exchange, AvatarText: &text, Color: &red})
	assert.Equal(t, "Exchange", set.Type.Value())
	assert.Equal(t, "NX", *set.AvatarText)
	assert.Equal(t, "#ff0000", set.Color.Value())
	assert.True(t, model.PlatformSettings{Color: &red}.Customized())
	assert.False(t, model.PlatformSettings{Key: "x"}.Customized())
}

func TestHoldingEffectiveReturn(t *testing.T) {
	own, class := decimal.NewFromInt(4), decimal.NewFromInt(9)
	assert.Nil(t, model.Holding{}.EffectiveReturnPct())
	assert.Equal(t, &class, model.Holding{ClassReturnPct: &class}.EffectiveReturnPct())
	assert.Equal(t, &own, model.Holding{ExpectedReturnPct: &own, ClassReturnPct: &class}.EffectiveReturnPct())
	assert.Empty(t, strings.TrimSpace(""))
}

func ptr[T any](v T) *T { return &v }

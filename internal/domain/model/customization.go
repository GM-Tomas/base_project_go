package model

import (
	"cmp"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/shopspring/decimal"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// How users set up their asset classes and platforms: an override per name, kept apart from the holdings
// (a class or a platform still appears with its first holding), so nothing has to be seeded per account and
// a platform keeps its look when it's emptied and used again.

const (
	MaxClassSettingsPerUser    = 100
	MaxPlatformSettingsPerUser = 1000
)

var ErrUnknownId = errors.New("not one of the ids this API gives")

// AssetClassSettings is what a user set for one of their classes. A class they created is kept by its
// settings, so it shows even before it has holdings; a default class they removed is kept Hidden, so it
// doesn't come back (creating it again brings it back).
type AssetClassSettings struct {
	UserId UserId
	Name   AssetClass
	// Color is the class's in charts and labels; nil is its default one.
	Color *Color
	// Liquid says whether it counts as ready to spend; nil is as by default (see ClassDefaults).
	Liquid *bool
	// ExpectedReturnPct is the yearly return its holdings without one of their own count with.
	ExpectedReturnPct *decimal.Decimal
	Hidden            bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Customized is whether anything about the class differs from its defaults.
func (s AssetClassSettings) Customized() bool {
	return s.Hidden || s.Color != nil || s.Liquid != nil || s.ExpectedReturnPct != nil
}

// PlatformSettings is how a user set up one of their platforms, by its key (see PlatformKey): kept when the
// platform is left without holdings, so it looks the same when used again.
type PlatformSettings struct {
	UserId UserId
	Key    string
	// Type replaces the one it would have (stored by earlier versions, or Other).
	Type *PlatformType
	// AvatarText, Color and TextColor are its thumbnail's; nil is the default (its initial, a color from its
	// name, its letters in that color).
	AvatarText *string
	Color      *Color
	TextColor  *Color
	UpdatedAt  time.Time
}

// Customized is whether anything about the platform is set (a document worth keeping).
func (s PlatformSettings) Customized() bool {
	return s.Type != nil || s.AvatarText != nil || s.Color != nil || s.TextColor != nil
}

// ClassDefaults is what classes are like until a user changes them: the classes every account starts with,
// in the order they're listed, and those that count as ready to spend.
type ClassDefaults struct {
	Names  []AssetClass
	Liquid []AssetClass
}

// Classes is a user's asset classes as they set them up: the defaults, with their settings on top.
type Classes struct {
	defaults ClassDefaults
	settings map[AssetClass]AssetClassSettings
}

func NewClasses(defaults ClassDefaults, settings []AssetClassSettings) Classes {
	byName := make(map[AssetClass]AssetClassSettings, len(settings))
	for _, s := range settings {
		byName[s.Name] = s
	}
	return Classes{defaults: defaults, settings: byName}
}

// Settings is what the user set for the class, if anything.
func (c Classes) Settings(class AssetClass) (AssetClassSettings, bool) {
	s, ok := c.settings[class]
	return s, ok
}

func (c Classes) IsDefault(class AssetClass) bool {
	return slices.Contains(c.defaults.Names, class)
}

// Liquid is whether the class counts as ready to spend: as the user set it, or as by default.
func (c Classes) Liquid(class AssetClass) bool {
	if s, ok := c.settings[class]; ok && s.Liquid != nil {
		return *s.Liquid
	}
	return slices.Contains(c.defaults.Liquid, class)
}

// ReturnPct is the yearly return the class's holdings without one of their own count with, if set.
func (c Classes) ReturnPct(class AssetClass) *decimal.Decimal {
	return c.settings[class].ExpectedReturnPct
}

// Color is the class's color as the user set it; nil is its default one.
func (c Classes) Color(class AssetClass) *Color {
	return c.settings[class].Color
}

// Visible lists the classes the user has, given those their holdings use: the defaults they didn't remove,
// in their order, then the ones they created and the ones in use, by name (a removed default in use is
// still there: its holdings have it).
func (c Classes) Visible(inUse []AssetClass) []AssetClass {
	seen := make(map[AssetClass]bool)
	var visible, others []AssetClass
	for _, d := range c.defaults.Names {
		if !seen[d] && !c.settings[d].Hidden {
			seen[d] = true
			visible = append(visible, d)
		}
	}
	add := func(class AssetClass) {
		if !seen[class] {
			seen[class] = true
			others = append(others, class)
		}
	}
	for _, s := range c.settings {
		if !s.Hidden {
			add(s.Name)
		}
	}
	for _, class := range inUse {
		add(class)
	}
	slices.SortFunc(others, func(a, b AssetClass) int {
		return cmp.Or(strings.Compare(SortName(a.Value()), SortName(b.Value())), strings.Compare(a.Value(), b.Value()))
	})
	return append(visible, others...)
}

// WithClassReturns is each holding's return as it counts: its own, or its class's default.
func (c Classes) WithClassReturns(returns []HoldingReturn) []HoldingReturn {
	out := make([]HoldingReturn, len(returns))
	for i, r := range returns {
		if r.Pct == nil {
			r.Pct = c.ReturnPct(r.Class)
		}
		out[i] = r
	}
	return out
}

// AssetClassId is how the API names a class in its paths: its name (as classes are kept, in NFC), base64url.
func AssetClassId(class AssetClass) string {
	return base64.RawURLEncoding.EncodeToString([]byte(class.Value()))
}

// ParseAssetClassId is the class an id names.
func ParseAssetClassId(id string) (AssetClass, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return AssetClass{}, ErrUnknownId
	}
	class, err := NewAssetClass(string(raw))
	if err != nil || class.Value() != string(raw) {
		return AssetClass{}, ErrUnknownId
	}
	return class, nil
}

// PlatformKey is what makes two platform names the same platform: Unicode's canonical caseless match
// ("Binance" and "binance", "Straße" and "STRASSE", "Café" typed with a precomposed "é" or with "e" and a
// combining accent).
func PlatformKey(name PlatformName) string {
	folder := folders.Get().(*cases.Caser)
	defer folders.Put(folder)
	return norm.NFD.String(folder.String(norm.NFD.String(name.Value())))
}

// folders reuses case folders, which aren't safe for concurrent use, across the many PlatformKey calls a
// listing makes.
var folders = sync.Pool{New: func() any {
	folder := cases.Fold()
	return &folder
}}

// SortName is how a label sorts as a person reads it: case and accents ignored, so "Álamo" goes with the
// A's rather than after "Zurich".
func SortName(label string) string {
	unaccented := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(label))
	folder := folders.Get().(*cases.Caser)
	defer folders.Put(folder)
	return folder.String(norm.NFC.String(unaccented))
}

// PlatformId is how the API names a platform in its paths: its key, base64url.
func PlatformId(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

// ParsePlatformKey is the platform key an id names.
func ParsePlatformKey(id string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(raw) == 0 {
		return "", ErrUnknownId
	}
	return string(raw), nil
}

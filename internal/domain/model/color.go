package model

import (
	"errors"
	"regexp"
	"strings"
)

var (
	hexColor        = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	ErrInvalidColor = errors.New("color must be a hex color like #1a2b3c")
)

// Color is a hex RGB color ("#c86bd6"), kept lowercase.
type Color struct {
	value string
}

func NewColor(raw string) (Color, error) {
	if !hexColor.MatchString(raw) {
		return Color{}, ErrInvalidColor
	}
	return Color{value: strings.ToLower(raw)}, nil
}

func (c Color) Value() string {
	return c.value
}

func MustColor(raw string) Color {
	c, err := NewColor(raw)
	if err != nil {
		panic(err)
	}
	return c
}

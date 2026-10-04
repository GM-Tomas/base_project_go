package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var (
	internalWhitespaceRegex = regexp.MustCompile(`\s+`)
	ErrBlankLabel           = errors.New("must not be blank")
	ErrLabelTooLong         = errors.New("exceeds max length")
)

// NormalizeLabel trims, collapses internal whitespace, validates length, and puts the label in Unicode
// NFC, so "é" typed precomposed or as "e" plus a combining accent is the same label, of the same length.
// NFC also decomposes a few letters (some Indic letters with a nukta, Hebrew presentation forms), making
// them longer: a label NFC would push past the limit is kept as given if that fits, so every label that
// ever passed still does.
func NormalizeLabel(raw string, maxLength int, fieldName string) (string, error) {
	label := internalWhitespaceRegex.ReplaceAllString(strings.TrimSpace(raw), " ")
	if label == "" {
		return "", fmt.Errorf("%s %w", fieldName, ErrBlankLabel)
	}
	if nfc := norm.NFC.String(label); utf8.RuneCountInString(nfc) <= maxLength {
		return nfc, nil
	}
	if n := utf8.RuneCountInString(label); n > maxLength {
		return "", fmt.Errorf("%s %w (%d > %d)", fieldName, ErrLabelTooLong, n, maxLength)
	}
	return label, nil
}

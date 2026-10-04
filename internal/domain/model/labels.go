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

// NormalizeLabel puts a label in Unicode NFC (so "é" typed precomposed or as "e" plus a combining accent
// is the same label, of the same length), trims it, collapses internal whitespace, and validates length.
func NormalizeLabel(raw string, maxLength int, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(norm.NFC.String(raw))
	normalized := internalWhitespaceRegex.ReplaceAllString(trimmed, " ")
	if normalized == "" {
		return "", fmt.Errorf("%s %w", fieldName, ErrBlankLabel)
	}
	if utf8.RuneCountInString(normalized) > maxLength {
		return "", fmt.Errorf("%s %w (%d > %d)", fieldName, ErrLabelTooLong, utf8.RuneCountInString(normalized), maxLength)
	}
	return normalized, nil
}

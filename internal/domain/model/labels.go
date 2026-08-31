package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	internalWhitespaceRegex = regexp.MustCompile(`\s+`)
	ErrBlankLabel           = errors.New("must not be blank")
	ErrLabelTooLong         = errors.New("exceeds max length")
)

// NormalizeLabel trims, collapses internal whitespace, and validates length.
func NormalizeLabel(raw string, maxLength int, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	normalized := internalWhitespaceRegex.ReplaceAllString(trimmed, " ")
	if normalized == "" {
		return "", fmt.Errorf("%s %w", fieldName, ErrBlankLabel)
	}
	if utf8.RuneCountInString(normalized) > maxLength {
		return "", fmt.Errorf("%s %w (%d > %d)", fieldName, ErrLabelTooLong, utf8.RuneCountInString(normalized), maxLength)
	}
	return normalized, nil
}

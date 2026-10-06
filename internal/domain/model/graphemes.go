package model

import (
	"errors"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var ErrInvalidAvatarText = errors.New("avatarText must be 1 or 2 characters (an emoji counts as one)")

// GraphemeCount is how many characters a person sees in s: combining marks, variation selectors, skin tones
// and keycaps join the character before them, a zero-width joiner joins the next one to it (the family and
// profession emoji), and two regional indicators make one flag. Enough for avatars: letters, digits and
// emoji.
func GraphemeCount(s string) int {
	count := 0
	joined, pendingFlag := false, false
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc), r == 0xFE0E, r == 0xFE0F, r == 0x20E3,
			r >= 0x1F3FB && r <= 0x1F3FF, r >= 0xE0020 && r <= 0xE007F:
			if count == 0 {
				count = 1
			}
		case r == 0x200D:
			joined = true
			if count == 0 {
				count = 1
			}
		case joined:
			joined = false
		case r >= 0x1F1E6 && r <= 0x1F1FF:
			if pendingFlag {
				pendingFlag = false
				continue
			}
			pendingFlag = true
			count++
		default:
			pendingFlag = false
			count++
		}
	}
	return count
}

// NewAvatarText is what a platform's thumbnail shows: 1 or 2 characters (an emoji counts as one), without
// the spaces around them.
func NewAvatarText(raw string) (string, error) {
	text := norm.NFC.String(strings.TrimSpace(raw))
	if n := GraphemeCount(text); n < 1 || n > 2 {
		return "", ErrInvalidAvatarText
	}
	return text, nil
}

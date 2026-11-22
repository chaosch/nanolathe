package i18n

import (
	"unicode"
)

func HasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303F) {
			return true
		}
	}
	return false
}

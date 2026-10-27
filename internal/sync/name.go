package sync

import (
	"strings"
	"unicode"
)

// DisplayName converts a key such as "support-agent" to "Support agent".
func DisplayName(key string) string {
	runes := []rune(strings.NewReplacer("-", " ", "_", " ").Replace(key))
	if len(runes) != 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

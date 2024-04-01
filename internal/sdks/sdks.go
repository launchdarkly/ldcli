package sdks

import (
	"strings"
)

// ReplaceFlagKey changes the placeholder flag key in the SDK instructions to the flag key from
// the user.
func ReplaceFlagKey(instructions string, key string) string {
	r := strings.NewReplacer(
		"my-flag-key",
		key,
		"my-flag",
		key,
		"my-boolean-flag",
		key,
		"FLAG_KEY",
		key,
		"<flag key>",
		key,
	)

	return r.Replace(instructions)
}

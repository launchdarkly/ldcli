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

// ReplaceSDKKey changes the placeholder flag key in the SDK instructions to the flag key from
// the user.
func ReplaceSDKKey(instructions string, key string) string {
	r := strings.NewReplacer(
		"1234567890abcdef",
		key,
		"myClientSideID",
		key,
		"mobile-key-from-launch-darkly-website",
		key,
		"YOUR_SDK_KEY",
		key,
		"Your LaunchDarkly SDK key",
		key,
	)

	return r.Replace(instructions)
}

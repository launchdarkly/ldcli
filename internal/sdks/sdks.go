package sdks

import (
	"embed"
	"regexp"
	"strings"
)

//go:embed sdk_instructions/*.md
var InstructionFiles embed.FS

// ReplaceFlagKey changes the placeholder flag key in the SDK instructions to the flag key from
// the user.
func ReplaceFlagKey(instructions string, key string) string {
	r := strings.NewReplacer(
		"my-flag-key",
		key,
		"myFlagKey",
		kebabToCamel(key),
		// remove remaining keys when we add all hardcoded instructions
		"my-flag",
		key,
		"my-boolean-flag",
		key,
		"<flag key>",
		key,
	)

	return r.Replace(instructions)
}

// ReplaceSDKKeys changes the placeholder SDK key/client side ID in the SDK instructions to the key from
// the default test environment for the user's account.
func ReplaceSDKKeys(instructions string, sdkKey, clientSideId, mobileKey string) string {
	r := strings.NewReplacer(
		"1234567890abcdef",
		sdkKey,
		"myClientSideId",
		clientSideId,
		"myMobileKey",
		mobileKey,
		// remove remaining values when we add all hardcoded instructions
		"mobile-key-from-launch-darkly-website",
		sdkKey,
		"YOUR_SDK_KEY",
		sdkKey,
		"Your LaunchDarkly SDK key",
		sdkKey,
		"myClientSideID",
		clientSideId,
	)

	return r.Replace(instructions)
}

// kebabToCamel converts a kebab-case key string into a camelCase key string, used for the React sdk instructions
func kebabToCamel(kebabCase string) string {
	replaceDashRegex := regexp.MustCompile(`-(.)`)
	camelCase := replaceDashRegex.ReplaceAllStringFunc(kebabCase, func(match string) string {
		return strings.ToUpper(string(match[1]))
	})
	return camelCase
}

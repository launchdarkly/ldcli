package errors

import "strings"

var suggestions = map[int]string{
	401: "Your access token may be invalid or expired. Run `ldcli login` or set LD_ACCESS_TOKEN. " +
		"Create a new token at {baseURI}/settings/authorization.",
	403: "You don't have permission for this action. Check your access token's role " +
		"and custom role policies in LaunchDarkly settings.",
	404: "Resource not found. Verify the project key, flag key, or environment key. " +
		"Use `ldcli projects list` or `ldcli flags list` to see available resources.",
	409: "Conflict: the resource may have been modified since you last read it. " +
		"Fetch the latest version and retry.",
	429: "Rate limited. Wait a moment and retry. If this persists, check your account's " +
		"rate limit allocation.",
}

func SuggestionForStatus(statusCode int, baseURI string) string {
	s, ok := suggestions[statusCode]
	if !ok {
		return ""
	}
	return strings.ReplaceAll(s, "{baseURI}", baseURI)
}

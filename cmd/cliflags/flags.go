package cliflags

const (
	BaseURIDefault = "https://app.launchdarkly.com"

	AccessTokenFlag = "access-token"
	AnalyticsOptOut = "analytics-opt-out"
	BaseURIFlag     = "base-uri"
	DataFlag        = "data"
	EmailsFlag      = "emails"
	EnvironmentFlag = "environment"
	FlagFlag        = "flag"
	OutputFlag      = "output"
	ProjectFlag     = "project"
	RoleFlag        = "role"

	AccessTokenFlagDescription = "LaunchDarkly access token with write-level access"
	AnalyticsOptOutDescription = "Opt out of analytics tracking"
	BaseURIFlagDescription     = "LaunchDarkly base URI"
	OutputFlagDescription      = "Command response output format in either JSON or plain text"
)

func AllFlagsHelp() map[string]string {
	return map[string]string{
		AccessTokenFlag: AccessTokenFlagDescription,
		AnalyticsOptOut: AnalyticsOptOutDescription,
		BaseURIFlag:     BaseURIFlagDescription,
		OutputFlag:      OutputFlagDescription,
	}
}

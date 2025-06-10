package cliflags

const (
	BaseURIDefault      = "https://app.launchdarkly.com"
	DevStreamURIDefault = "https://stream.launchdarkly.com"
	PortDefault         = "8765"

	AccessTokenFlag  = "access-token"
	AnalyticsOptOut  = "analytics-opt-out"
	BaseURIFlag      = "base-uri"
	DatabasePathFlag = "database-path"
	DataFlag         = "data"
	DevStreamURIFlag = "dev-stream-uri"
	EmailsFlag       = "emails"
	EnvironmentFlag  = "environment"
	FlagFlag         = "flag"
	OutputFlag       = "output"
	PortFlag         = "port"
	ProjectFlag      = "project"
	RoleFlag         = "role"
	SyncOnceFlag     = "sync-once"

	AccessTokenFlagDescription  = "LaunchDarkly access token with write-level access"
	AnalyticsOptOutDescription  = "Opt out of analytics tracking"
	BaseURIFlagDescription      = "LaunchDarkly base URI"
	DatabasePathFlagDescription = "Path to the database file for the dev server"
	DevStreamURIDescription     = "Streaming service endpoint that the dev server uses to obtain authoritative flag data. This may be a LaunchDarkly or Relay Proxy endpoint"
	EnvironmentFlagDescription  = "Default environment key"
	FlagFlagDescription         = "Default feature flag key"
	OutputFlagDescription       = "Command response output format in either JSON or plain text"
	PortFlagDescription         = "Port for the dev server to run on"
	ProjectFlagDescription      = "Default project key"
	SyncOnceFlagDescription     = "Only sync new projects. Existing projects will neither be resynced nor have overrides specified by CLI flags applied."
)

func AllFlagsHelp() map[string]string {
	return map[string]string{
		AccessTokenFlag:  AccessTokenFlagDescription,
		AnalyticsOptOut:  AnalyticsOptOutDescription,
		BaseURIFlag:      BaseURIFlagDescription,
		DatabasePathFlag: DatabasePathFlagDescription,
		DevStreamURIFlag: DevStreamURIDescription,
		EnvironmentFlag:  EnvironmentFlagDescription,
		FlagFlag:         FlagFlagDescription,
		OutputFlag:       OutputFlagDescription,
		PortFlag:         PortFlagDescription,
		ProjectFlag:      ProjectFlagDescription,
		SyncOnceFlag:     SyncOnceFlagDescription,
	}
}

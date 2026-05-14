package cliflags

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// GetOutputKind returns the effective output kind, giving precedence to --json over --output.
func GetOutputKind(cmd *cobra.Command) string {
	if jsonFlag, err := cmd.Root().PersistentFlags().GetBool(JSONFlag); err == nil && jsonFlag {
		return "json"
	}
	return viper.GetString(OutputFlag)
}

// GetFields returns the list of fields to include in JSON output, or nil if not specified.
func GetFields(cmd *cobra.Command) []string {
	fields, err := cmd.Root().PersistentFlags().GetStringSlice(FieldsFlag)
	if err != nil {
		return nil
	}
	return fields
}

const (
	BaseURIDefault      = "https://app.launchdarkly.com"
	DevStreamURIDefault = "https://stream.launchdarkly.com"
	PortDefault         = "8765"

	AccessTokenFlag        = "access-token"
	AllFlag                = "all"
	AnalyticsOptOut        = "analytics-opt-out"
	BaseURIFlag            = "base-uri"
	CorsEnabledFlag        = "cors-enabled"
	CorsOriginFlag         = "cors-origin"
	DataFlag               = "data"
	DetailedFlag           = "detailed"
	DryRunFlag             = "dry-run"
	DevStreamURIFlag       = "dev-stream-uri"
	EmailsFlag             = "emails"
	EnvironmentFlag        = "environment"
	FieldsFlag             = "fields"
	FlagFlag               = "flag"
	JSONFlag               = "json"
	LimitFlag              = "limit"
	OriginalVariationFlag  = "original-variation"
	OutputFlag             = "output"
	PauseOnRegressionFlag  = "pause-on-regression"
	PortFlag               = "port"
	ProjectFlag            = "project"
	RandomizationUnitFlag  = "randomization-unit"
	RevertOnRegressionFlag = "revert-on-regression"
	RoleFlag               = "role"
	RolloutIdFlag          = "rollout-id"
	RuleIDFlag             = "rule-id"
	StagesFlag             = "stages"
	SyncOnceFlag           = "sync-once"
	TargetVariationFlag    = "target-variation"
	ToVariationFlag        = "to-variation"

	AccessTokenFlagDescription        = "LaunchDarkly access token with write-level access"
	AllFlagDescription                = "Return all rollouts (ignores --limit; subject to upstream API limits per API-PAPERCUTS.md PC-003)"
	AnalyticsOptOutDescription        = "Opt out of analytics tracking"
	BaseURIFlagDescription            = "LaunchDarkly base URI"
	CorsEnabledFlagDescription        = "Enable CORS headers for browser-based developer tools (default: false)"
	CorsOriginFlagDescription         = "Allowed CORS origin. Use '*' for all origins (default: '*')"
	DetailedFlagDescription           = "Plaintext only: include variations, ended-at, current stage index, raw API status. Ignored in JSON output (which always emits the full field set)."
	DevStreamURIDescription           = "Streaming service endpoint that the dev server uses to obtain authoritative flag data. This may be a LaunchDarkly or Relay Proxy endpoint"
	DryRunFlagDescription             = "Validate the change without persisting it. Returns a preview of the result."
	EnvironmentFlagDescription        = "Default environment key"
	FieldsFlagDescription             = "Comma-separated list of top-level fields to include in JSON output (e.g., --fields key,name,kind)"
	FlagFlagDescription               = "Default feature flag key"
	JSONFlagDescription               = "Output JSON format (shorthand for --output json)"
	LimitFlagDescription              = "Maximum number of rollouts to return (default 20; ignored when --all is set)"
	OriginalVariationFlagDescription  = "The variation UUID (_id) that represents the original/control variation. Obtain via: ldcli flags get --flag <key> --output json | jq '.variations[]'"
	OutputFlagDescription             = "Output format: json, plaintext, or markdown (default: plaintext in a terminal, json otherwise)"
	PauseOnRegressionFlagDescription  = "Metric key to monitor; pauses the rollout at current stage on regression (repeatable). Use --revert-on-regression to auto-rollback instead. A metric cannot appear in both flags."
	PortFlagDescription               = "Port for the dev server to run on"
	ProjectFlagDescription            = "Default project key"
	RandomizationUnitFlagDescription  = "Randomization unit for the experiment (e.g. user, organization)"
	RevertOnRegressionFlagDescription = "Metric key to monitor; automatically reverts the rollout on regression (repeatable). Use --pause-on-regression to pause instead. A metric cannot appear in both flags."
	RolloutIdFlagDescription          = "Specific rollout UUID to inspect; when set, --environment is also required (API-PAPERCUTS.md PC-004 — GET-by-ID requires environmentKey in the URL path). Omit this flag to get the most-recent rollout on the flag."
	RuleIDFlagDescription             = "Existing rule UUID to roll out on. Omit for fallthrough (default rule). Obtain rule IDs from the LaunchDarkly UI or API."
	StagesFlagDescription             = "Comma-separated list of stages as <allocation%>:<duration> (e.g. 25:60m,50:60m,100:60m). Allocation must be a whole percent integer [1-100]; duration must include a unit (60m, 1h30m, 300s). The CLI converts allocation to basis-points and duration to milliseconds for the API."
	SyncOnceFlagDescription           = "Only sync new projects. Existing projects will neither be resynced nor have overrides specified by CLI flags applied."
	TargetVariationFlagDescription    = "The variation UUID (_id) that traffic will be shifted to. Obtain via: ldcli flags get --flag <key> --output json | jq '.variations[]'"
	ToVariationFlagDescription        = "Final variation UUID (_id) the rollout will conclude on. Pass either the original variation (control) to roll back, or the target variation (test) to roll forward. Obtain UUIDs via: ldcli flags get --flag <key> --output json | jq '.variations[]'"
)

// AllFlagsHelp returns the names + descriptions of flags that can be persisted as
// user-level configuration via `ldcli config --set <flag>=<value>`. Per-command flags that
// are only meaningful inside a single invocation (e.g. --all, --detailed, --limit on the
// list verb) are intentionally NOT included here — those are local to the command and
// should not pollute the config-file surface.
func AllFlagsHelp() map[string]string {
	return map[string]string{
		AccessTokenFlag:  AccessTokenFlagDescription,
		AnalyticsOptOut:  AnalyticsOptOutDescription,
		BaseURIFlag:      BaseURIFlagDescription,
		CorsEnabledFlag:  CorsEnabledFlagDescription,
		CorsOriginFlag:   CorsOriginFlagDescription,
		DevStreamURIFlag: DevStreamURIDescription,
		EnvironmentFlag:  EnvironmentFlagDescription,
		FlagFlag:         FlagFlagDescription,
		OutputFlag:       OutputFlagDescription,
		PortFlag:         PortFlagDescription,
		ProjectFlag:      ProjectFlagDescription,
		SyncOnceFlag:     SyncOnceFlagDescription,
	}
}

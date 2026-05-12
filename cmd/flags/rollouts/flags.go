package rollouts

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
)

// initListFlags registers the flags for the `list` verb.
//
// Required: --flag, --project (inherited from Plan 01; reuse the existing constants).
// Optional (Plan 03): --environment, --limit, --all, --detailed.
//
// Per D-04, --state is NOT registered (dropped from v1).
// Per the deferred-feature decision, --idempotency-key is NOT registered (Phase 2).
func initListFlags(cmd *cobra.Command) {
	cmd.Flags().String(cliflags.FlagFlag, "", "The feature flag key")
	_ = cmd.MarkFlagRequired(cliflags.FlagFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.FlagFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.FlagFlag, cmd.Flags().Lookup(cliflags.FlagFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(cliflags.EnvironmentFlag, "", "Filter rollouts by environment key (optional)")
	_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))

	cmd.Flags().Int(cliflags.LimitFlag, 20, cliflags.LimitFlagDescription)
	_ = viper.BindPFlag(cliflags.LimitFlag, cmd.Flags().Lookup(cliflags.LimitFlag))

	cmd.Flags().Bool(cliflags.AllFlag, false, cliflags.AllFlagDescription)
	_ = viper.BindPFlag(cliflags.AllFlag, cmd.Flags().Lookup(cliflags.AllFlag))

	cmd.Flags().Bool(cliflags.DetailedFlag, false, cliflags.DetailedFlagDescription)
	_ = viper.BindPFlag(cliflags.DetailedFlag, cmd.Flags().Lookup(cliflags.DetailedFlag))
}

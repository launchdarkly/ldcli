package rollouts

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/rollouts"
)

const longDescription = `Manage automated rollouts (guarded and progressive) for a feature flag.

THIS COMMAND TREE IS BETA. The surface and output schema may change between releases.
Pin to a specific ldcli version when used in CI/CD or production agent workflows.`

// NewRolloutsCmd builds the parent ` + "`flags rollouts-beta`" + ` command. Each verb (` + "`list`" + ` in
// Phase 1; ` + "`start`" + `, ` + "`status`" + `, ` + "`stop`" + ` in later phases) hangs off this parent. The
// PersistentPreRun emits the rollouts-beta analytics event and prints the beta banner to
// stderr when stderr is a TTY AND the effective output kind is not JSON.
func NewRolloutsCmd(client rollouts.Client, analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollouts-beta",
		Short: "Manage automated rollouts (beta)",
		Long:  longDescription,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
				cmd,
				"flags-rollouts-beta",
				map[string]interface{}{
					"action": cmd.Name(),
				},
			))

			if shouldPrintBetaBanner(cmd) {
				printBetaBanner(cmd)
			}
		},
	}

	cmd.AddCommand(NewListCmd(client))
	cmd.AddCommand(NewStartCmd(client))
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	return cmd
}

// shouldPrintBetaBanner returns true only when stderr is a TTY AND the effective output kind
// is not JSON. Per the banner contract, JSON output suppresses the banner (so consumers parsing
// stdout do not also need to scrub stderr) and non-TTY contexts suppress it too (CI logs do
// not need decoration).
//
// FORCE_TTY (or LD_FORCE_TTY) environment variable set to a non-empty value forces the TTY
// branch on — this mirrors the root command's FORCE_TTY semantics and makes the banner-on-TTY
// path testable inside `go test` (where stderr is typically a pipe, not a TTY).
func shouldPrintBetaBanner(cmd *cobra.Command) bool {
	if cliflags.GetOutputKind(cmd) == "json" {
		return false
	}
	if os.Getenv("FORCE_TTY") != "" || os.Getenv("LD_FORCE_TTY") != "" {
		return true
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// printBetaBanner writes the two-line banner to stderr. Copy is the authoritative wording from
// the plan's PATTERNS.md ("Banner copy" section). The CLI version is interpolated from
// cmd.Root().Version (empty string is acceptable — the banner remains readable).
func printBetaBanner(cmd *cobra.Command) {
	fmt.Fprintln(cmd.ErrOrStderr(), "⚠ flags rollouts-beta is unstable; the command surface and JSON schema may change.")
	fmt.Fprintf(cmd.ErrOrStderr(), "  Pin to ldcli %s for production use.\n", cmd.Root().Version)
}

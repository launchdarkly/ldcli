// Package awsdevopsagent provisions the AWS DevOps Agent so it can manage
// LaunchDarkly flags. Commands run against the caller's own AWS session.
package awsdevopsagent

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
)

const (
	regionFlag       = "region"
	agentSpaceIDFlag = "agent-space-id"
)

func NewAWSDevOpsAgentCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aws-devops-agent",
		Short: "Set up the AWS DevOps Agent",
		Long: `Provision the AWS DevOps Agent integration in your own AWS account.

These commands use your existing AWS session: authenticate first (for example
with "aws sso login" or by exporting AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
and AWS_SESSION_TOKEN) and they fail before changing anything if no credentials
are available.`,
		Args: cobra.MinimumNArgs(1),
	}

	cmd.AddCommand(NewSetupCmd(analyticsTrackerFn))
	cmd.AddCommand(NewStatusCmd(analyticsTrackerFn))
	cmd.AddCommand(NewTeardownCmd(analyticsTrackerFn))
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

// trackRun reports the run from PreRun rather than PersistentPreRun, which
// would shadow the root command's own PersistentPreRun.
func trackRun(analyticsTrackerFn analytics.TrackerFn) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		analyticsTrackerFn(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetBool(cliflags.AnalyticsOptOut),
		).SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
			cmd,
			"aws-devops-agent",
			map[string]interface{}{"action": cmd.Name()},
		))
	}
}

func printJSON(cmd *cobra.Command, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))

	return nil
}

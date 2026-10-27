package sync

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// NewSyncCmd creates the parent command for synchronization workflows.
func NewSyncCmd(client resources.Client, analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize local resources with LaunchDarkly",
		Example: `  # Synchronize prompt variations, tools, and skills
  ldcli sync all

  # Preview every change
  ldcli sync all --dry-run

  # Synchronize only tools
  ldcli sync tools`,
		Args: cobra.MinimumNArgs(1),
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
				cmd,
				"sync",
				map[string]interface{}{"action": cmd.Name(), "scope": commandScope(cmd)},
			))
		},
	}

	cmd.AddCommand(
		NewAllCmd(client),
		NewPromptCmd(client),
		newAttachmentScopeCmd(client, syncdomain.AttachmentTool),
		newAttachmentScopeCmd(client, syncdomain.AttachmentSkill),
	)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

// commandScope returns the scope of a sync command: all, prompts, tools, or
// skills. The name of the command is add for every kind, so analytics needs
// the scope too.
func commandScope(cmd *cobra.Command) string {
	for ; cmd.HasParent(); cmd = cmd.Parent() {
		if cmd.Parent().Name() == "sync" {
			return cmd.Name()
		}
	}
	return ""
}

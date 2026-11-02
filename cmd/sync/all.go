package sync

import (
	"github.com/spf13/cobra"

	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// NewAllCmd creates the command that syncs prompt variations, tools, and
// skills in one plan.
func NewAllCmd(client resources.Client) *cobra.Command {
	help := targetFlagHelp{project: filterFlagHelp.project}
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Synchronize prompt variations, tools, and skills with LaunchDarkly",
		Long: "Synchronize prompt variations, tools, and skills with LaunchDarkly in one plan. " +
			"Sync applies tools and skills before the prompt variations that use them. " +
			"One conflict choice covers every conflict. " +
			"To choose for one resource, use sync prompts, sync tools, or sync skills with --key.",
		Example: `  # Preview every change
  ldcli sync all --dry-run

  # Apply every change without prompting
  ldcli sync all --yes --no-input

  # Keep the local side of every conflict in one project
  ldcli sync all --project production --conflict=local --yes`,
		Args: validatedArgs(cobra.NoArgs),
		RunE: runCommand(client, syncdomain.ScopeAll, buildSyncAction),
	}
	addTargetFlags(cmd, help)
	addApplyFlags(cmd, syncdomain.ScopeAll, true)
	cmd.AddCommand(newWatchCmd(client, syncdomain.ScopeAll, help))
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	return cmd
}

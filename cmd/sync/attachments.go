package sync

import (
	"fmt"

	"github.com/spf13/cobra"

	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

// attachmentExamples are the keys that the help of a tool or skill command
// uses in its examples.
var attachmentExamples = map[syncdomain.AttachmentKind][2]string{
	syncdomain.AttachmentTool:  {"search", "lookup-order"},
	syncdomain.AttachmentSkill: {"summarize", "triage"},
}

// newAttachmentScopeCmd creates the command that syncs the tools or the
// skills of the repository, with its watch, add, and detach commands. They
// run the same sync as sync prompts, in the scope of kind.
func newAttachmentScopeCmd(client resources.Client, kind syncdomain.AttachmentKind) *cobra.Command {
	scope := syncdomain.Scope(kind + "s")
	key := attachmentExamples[kind][0]
	cmd := &cobra.Command{
		Use:   string(scope),
		Short: fmt.Sprintf("Synchronize local %s files with LaunchDarkly", kind),
		Long:  attachmentScopeLong(kind),
		Example: fmt.Sprintf(`  # Preview %[1]s changes
  ldcli sync %[2]s --dry-run

  # Apply %[1]s changes without prompting
  ldcli sync %[2]s --yes --no-input

  # Keep the local version of one %[1]s when it conflicts with LaunchDarkly
  ldcli sync %[2]s --project production --key %[3]s --conflict=local --yes`, kind, scope, key),
		Args: validatedArgs(cobra.NoArgs),
		RunE: runCommand(client, scope, buildSyncAction),
	}
	help := attachmentFlagHelp(kind, true)
	addTargetFlags(cmd, help)
	addApplyFlags(cmd, scope, true)
	cmd.AddCommand(
		newWatchCmd(client, scope, help),
		newAttachmentAddCmd(client, kind),
		newAttachmentDetachCmd(client, kind),
	)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	return cmd
}

// attachmentScopeLong is the description of sync tools or sync skills.
func attachmentScopeLong(kind syncdomain.AttachmentKind) string {
	upsert := `set "upsert": true in its file`
	name := ""
	if kind == syncdomain.AttachmentSkill {
		upsert = "set upsert: true in its front matter"
		name = " A skill that sync creates from a file takes its name from name: in the file, or from its key."
	}
	return fmt.Sprintf("Synchronize local %[1]s files with LaunchDarkly. A %[1]s syncs on its own, also when no prompt variation uses it. "+
		"To create a %[1]s, %[2]s.%[3]s Sync rechecks state before every write. If the state changed, run sync again.", kind, upsert, name)
}

func newAttachmentAddCmd(client resources.Client, kind syncdomain.AttachmentKind) *cobra.Command {
	scope := syncdomain.Scope(kind + "s")
	keys := attachmentExamples[kind]
	cmd := &cobra.Command{
		Use:   "add",
		Short: fmt.Sprintf("Add %ss from LaunchDarkly", kind),
		Long: fmt.Sprintf("Add %[1]ss from LaunchDarkly. Add writes the file of each %[1]s and records it in sync.lock and "+
			"in the LaunchDarkly manifest. Each %[1]s must exist in LaunchDarkly.", kind),
		Example: fmt.Sprintf(`  # Choose a project and a %[1]s interactively
  ldcli sync %[2]s add

  # Add %[1]ss by key without prompting
  ldcli sync %[2]s add --project production --key %[3]s --key %[4]s --no-input`, kind, scope, keys[0], keys[1]),
		Args: validatedArgs(cobra.NoArgs),
		RunE: runCommand(client, scope, func(cmd *cobra.Command, _ []string) (syncprompt.CommandAction, error) {
			dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
			return syncprompt.AddAction{Filter: targetFilter(cmd), DryRun: dryRun}, nil
		}),
	}
	addTargetFlags(cmd, attachmentFlagHelp(kind, false))
	cmd.Flags().Bool(dryRunFlag, false, "Preview local files without creating them")
	cmd.Flags().Bool(diffFlag, false, "Show full diffs and file content in the plan")
	cmd.Flags().Bool(noInputFlag, false, "Fail instead of prompting for missing input")
	return cmd
}

func newAttachmentDetachCmd(client resources.Client, kind syncdomain.AttachmentKind) *cobra.Command {
	scope := syncdomain.Scope(kind + "s")
	cmd := &cobra.Command{
		Use:   "detach",
		Short: fmt.Sprintf("Stop syncing local %ss", kind),
		Long: fmt.Sprintf("Stop syncing local %[1]ss. Detach deletes the local %[1]s file and its sync.lock entry. "+
			"LaunchDarkly keeps the %[1]s. Detach cannot remove a %[1]s that a synced prompt variation uses. "+
			"Remove it from the variation first. Detach asks before it deletes files. Without a terminal, pass --yes.", kind),
		Example: fmt.Sprintf(`  # Choose %[1]ss interactively
  ldcli sync %[2]s detach

  # Stop syncing one %[1]s without prompting
  ldcli sync %[2]s detach --project production --key %[3]s --yes`, kind, scope, attachmentExamples[kind][0]),
		Args: validatedArgs(cobra.NoArgs),
		RunE: runCommand(client, scope, func(cmd *cobra.Command, _ []string) (syncprompt.CommandAction, error) {
			return syncprompt.DetachAction{Filter: targetFilter(cmd)}, nil
		}),
	}
	addTargetFlags(cmd, attachmentFlagHelp(kind, false))
	cmd.Flags().Bool(yesFlag, false, "Detach without confirmation")
	cmd.Flags().Bool(noInputFlag, false, "Fail instead of prompting for missing input")
	return cmd
}

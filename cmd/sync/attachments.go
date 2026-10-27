package sync

import (
	"fmt"

	"github.com/spf13/cobra"

	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synccreate "github.com/launchdarkly/ldcli/internal/sync/create"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

// attachmentExamples are the keys that the help of a tool or skill command
// uses in its examples.
var attachmentExamples = map[syncdomain.AttachmentKind][2]string{
	syncdomain.AttachmentTool:  {"search", "lookup-order"},
	syncdomain.AttachmentSkill: {"summarize", "triage"},
}

// createExamples are the examples of an add that creates a tool or a skill.
var createExamples = map[syncdomain.AttachmentKind]string{
	syncdomain.AttachmentTool: `  # Create a tool without prompting
  ldcli sync tools add --project production --key refund \
    --description "Issue a refund" \
    --json-schema '{"type":"object","properties":{"orderId":{"type":"string"}},"required":["orderId"]}' \
    --yes --no-input`,
	syncdomain.AttachmentSkill: `  # Create a skill from a Markdown file without prompting
  ldcli sync skills add --project production --key handoff \
    --name "Human handoff" \
    --description "Hand a conversation to a person" \
    --content "$(cat handoff.md)" \
    --yes --no-input`,
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
		"To create a %[1]s, run sync %[1]ss add with %[4]s, or %[2]s.%[3]s "+
		"Sync rechecks state before every write. If the state changed, run sync again.", kind, upsert, name, createFlag(kind))
}

// attachmentAddLong is the description of sync tools add or sync skills add.
func attachmentAddLong(kind syncdomain.AttachmentKind) string {
	content, skill := "schema", ""
	if kind == syncdomain.AttachmentSkill {
		content, skill = "content", " --content is the skill Markdown. The name comes from --name, or from the key."
	}
	return fmt.Sprintf("Add %[1]ss from LaunchDarkly, or create a %[1]s. "+
		"Without %[2]s, add starts syncing %[1]ss that exist in LaunchDarkly. "+
		"With %[2]s, add creates the %[1]s if it does not exist.%[4]s "+
		"If the %[1]s exists with the same %[3]s and description, add starts syncing it. "+
		"If they differ, add stops and changes nothing.", kind, createFlag(kind), content, skill)
}

// createFlag is the flag that starts the create of a tool or a skill.
func createFlag(kind syncdomain.AttachmentKind) string {
	if kind == syncdomain.AttachmentTool {
		return "--" + jsonSchemaFlag
	}
	return "--" + contentFlag
}

func newAttachmentAddCmd(client resources.Client, kind syncdomain.AttachmentKind) *cobra.Command {
	scope := syncdomain.Scope(kind + "s")
	keys := attachmentExamples[kind]
	cmd := &cobra.Command{
		Use:   "add",
		Short: fmt.Sprintf("Add %ss from LaunchDarkly, or create a %s", kind, kind),
		Long:  attachmentAddLong(kind),
		Example: fmt.Sprintf(`  # Choose a project and a %[1]s interactively
  ldcli sync %[2]s add

  # Add %[1]ss by key without prompting
  ldcli sync %[2]s add --project production --key %[3]s --key %[4]s --no-input

%[5]s`, kind, scope, keys[0], keys[1], createExamples[kind]),
		Args: validatedArgs(cobra.NoArgs),
		RunE: runCommand(client, scope, func(cmd *cobra.Command, _ []string) (syncprompt.CommandAction, error) {
			dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
			return syncprompt.AddAction{Filter: targetFilter(cmd), DryRun: dryRun, Create: createInput(cmd)}, nil
		}),
	}
	addTargetFlags(cmd, attachmentFlagHelp(kind, false))
	if kind == syncdomain.AttachmentTool {
		cmd.Flags().String(jsonSchemaFlag, "", "Input JSON Schema of a tool that add creates")
	} else {
		cmd.Flags().String(contentFlag, "", "Markdown of a skill that add creates")
		cmd.Flags().String(nameFlag, "", "Name of a skill that add creates. The default comes from the key.")
	}
	cmd.Flags().String(descriptionFlag, "", fmt.Sprintf("Description of a %s that add creates", kind))
	cmd.Flags().Bool(dryRunFlag, false, "Preview local files without creating them")
	cmd.Flags().Bool(diffFlag, false, "Show full diffs and file content in the plan")
	cmd.Flags().Bool(yesFlag, false, "Create without confirmation")
	cmd.Flags().Bool(noInputFlag, false, "Fail instead of prompting for missing input")
	return cmd
}

// createInput reads the create flags of add. It is nil when the command line
// sets none of them, so that add then adds only resources that LaunchDarkly
// has. A flag set to an empty value is set.
func createInput(cmd *cobra.Command) *synccreate.Input {
	set := false
	value := func(name string) *string {
		flag := cmd.Flags().Lookup(name)
		if flag == nil || !flag.Changed {
			return nil
		}
		set = true
		text := flag.Value.String()
		return &text
	}
	input := synccreate.Input{
		Content: value(contentFlag), JSONSchema: value(jsonSchemaFlag), Name: value(nameFlag), Description: value(descriptionFlag),
	}
	if !set {
		return nil
	}
	return &input
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

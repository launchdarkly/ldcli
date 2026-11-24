package sync

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

// linkExamples are the examples of the link command of each resource kind.
var linkExamples = map[syncdomain.Kind]string{
	syncdomain.KindVariation: `  # Link one Python agent to a prompt variation
  ldcli sync prompts link \
    --file app/agents.py \
    --format python \
    --select 'agents[id="support"]' \
    --map prompt=instructions \
    --project production --config-key support --key default \
    --yes --no-input

  # Create the config and the variation when LaunchDarkly has none
  ldcli sync prompts link \
    --file app/agents.py \
    --format python \
    --select 'agents[id="support"]' \
    --map prompt=instructions \
    --project production --config-key support --key default \
    --upsert --model-config-key gpt-5 \
    --yes --no-input`,
	syncdomain.KindTool: `  # Link one TypeScript MCP tool
  ldcli sync tools link \
    --file src/server.ts \
    --format typescript \
    --select 'tools[id="lookup_order"]' \
    --map description=description \
    --project production --key lookup_order \
    --yes --no-input`,
	syncdomain.KindSkill: `  # Link one Agent Skill file
  ldcli sync skills link \
    --file .agents/skills/refunds/SKILL.md \
    --format agent-skill \
    --project production --key refunds \
    --yes --no-input`,
}

// newLinkCmd creates the link command of one resource kind. Link records a
// .ref.yaml descriptor for a repository source and then syncs the resource.
func newLinkCmd(client resources.Client, kind syncdomain.Kind) *cobra.Command {
	scope, noun, help := syncdomain.ScopePrompts, "prompt variation", questionFlagHelp
	if kind != syncdomain.KindVariation {
		scope = syncdomain.Scope(kind + "s")
		noun = string(kind)
		help = attachmentFlagHelp(syncdomain.AttachmentKind(kind), false)
	}
	cmd := &cobra.Command{
		Use:   "link",
		Short: fmt.Sprintf("Link a repository source to one %s", noun),
		Long: fmt.Sprintf("Link a repository source to one %s. "+
			"Link reads only the named file, shows the descriptor and the sync plan, and writes the descriptor after you confirm. "+
			"It then syncs the %s. With --upsert, it creates the %s in LaunchDarkly when it does not exist, and the config of a variation too.", noun, noun, noun),
		Example: linkExamples[kind],
		Args:    validatedArgs(cobra.NoArgs),
		RunE: runCommand(client, scope, func(cmd *cobra.Command, _ []string) (syncprompt.CommandAction, error) {
			id, err := sourceLinkID(cmd, kind)
			if err != nil {
				return nil, err
			}
			fieldMap, err := parseFieldMappings(mustStringArray(cmd, mapFlag))
			if err != nil {
				return nil, err
			}
			upsert, _ := cmd.Flags().GetBool(upsertFlag)
			return syncprompt.SourceLinkAction{
				ID:             id,
				File:           mustString(cmd, fileFlag),
				Format:         mustString(cmd, formatFlag),
				Select:         mustString(cmd, selectFlag),
				Map:            fieldMap,
				Upsert:         upsert,
				ModelConfigKey: mustString(cmd, modelConfigKeyFlag),
				Mode:           syncdomain.VariationMode(mustString(cmd, modeFlag)),
			}, nil
		}),
	}
	addTargetFlags(cmd, help)
	cmd.Flags().String(fileFlag, "", "Repository source file")
	cmd.Flags().String(formatFlag, "", "Source format, for example python, typescript, markdown, or json")
	cmd.Flags().String(selectFlag, "", "Source value to select, for example 'agents[id=\"support\"]'")
	cmd.Flags().StringArray(mapFlag, nil, "Mapped field in field=path form. Repeat for more fields.")
	cmd.Flags().Bool(upsertFlag, false, fmt.Sprintf("Create the %s in LaunchDarkly when it does not exist", noun))
	if kind == syncdomain.KindVariation {
		cmd.Flags().String(modelConfigKeyFlag, "", "Model config of a variation that --upsert creates. The create uses its latest version.")
		cmd.Flags().String(modeFlag, "", "Mode of a config that --upsert creates: agent or completion. The default comes from the source.")
	}
	_ = cmd.MarkFlagRequired(fileFlag)
	_ = cmd.MarkFlagRequired(formatFlag)
	addApplyFlags(cmd, scope, false)
	return cmd
}

// sourceLinkID reads the resource that the target flags name. Link needs
// exactly one resource.
func sourceLinkID(cmd *cobra.Command, kind syncdomain.Kind) (syncdomain.ResourceID, error) {
	filter := targetFilter(cmd)
	if err := filter.Validate(kind); err != nil {
		return syncdomain.ResourceID{}, err
	}
	if filter.Project == "" {
		return syncdomain.ResourceID{}, errors.New("--project is required")
	}
	if len(filter.Keys) != 1 {
		return syncdomain.ResourceID{}, errors.New("link requires exactly one --key")
	}
	if kind == syncdomain.KindVariation {
		if filter.ConfigKey == "" {
			return syncdomain.ResourceID{}, errors.New("--config-key is required")
		}
		return syncdomain.VariationID(filter.Project, filter.ConfigKey, filter.Keys[0]), nil
	}
	return syncdomain.ResourceID{Kind: kind, ProjectKey: filter.Project, LookupKey: filter.Keys[0]}, nil
}

// parseFieldMappings reads the --map values. Each value is field=path.
func parseFieldMappings(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	fieldMap := make(map[string]string, len(values))
	for _, value := range values {
		field, path, ok := strings.Cut(value, "=")
		if !ok || field == "" || path == "" {
			return nil, fmt.Errorf("invalid --map %q; expected field=path", value)
		}
		if _, duplicate := fieldMap[field]; duplicate {
			return nil, fmt.Errorf("--map field %q was provided more than once", field)
		}
		fieldMap[field] = path
	}
	return fieldMap, nil
}

func mustString(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func mustStringArray(cmd *cobra.Command, name string) []string {
	value, _ := cmd.Flags().GetStringArray(name)
	return value
}

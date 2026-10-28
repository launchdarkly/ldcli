package awsdevopsagent

import (
	"errors"
	"fmt"
	"strings"

	agenttypes "github.com/aws/aws-sdk-go-v2/service/devopsagent/types"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const (
	agentSpaceNameFlag  = "agent-space-name"
	newAgentSpaceFlag   = "new-agent-space"
	authFlowFlag        = "auth-flow"
	fullSetupFlag       = "full-setup"
	idcInstanceARNFlag  = "idc-instance-arn"
	issuerURLFlag       = "issuer-url"
	idpClientIDFlag     = "idp-client-id"
	idpClientSecretFlag = "idp-client-secret"
	mcpServiceIDFlag    = "mcp-service-id"
	mcpEndpointFlag     = "mcp-endpoint"
	replaceSkillFlag    = "replace-skill"
	addMCPToolsFlag     = "add-mcp-tools"
	yesFlag             = "yes"
)

func NewSetupCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Provision the AWS DevOps Agent for LaunchDarkly",
		Long: `Create the IAM roles, agent space, AWS account association, operator app,
LaunchDarkly MCP server connection and experimentation skill the AWS DevOps
Agent needs.

When the account already has agent spaces, setup asks which one to connect
LaunchDarkly to. On a space that setup did not make, setup only registers the
MCP server and adds the skill. On a space that setup made, or with
--full-setup, setup runs the full setup. A full setup turns on elevated
actions on the space. Pass --agent-space-id to pick a space without the list,
or --new-agent-space to provision another space. A new space needs a name:
pass --agent-space-name, or type one when setup asks. The run ends with the
operator app URL, where the agent is used.

The AWS account gets one LaunchDarkly MCP server, which all agent spaces use.
If the account has no server, setup registers one with OAuth. It opens a
browser, where you approve access to LaunchDarkly, and waits until AWS
registers the server. AWS can ask you to sign in to the AWS console first.
The agent then acts as the LaunchDarkly member who approved access, in every
agent space. If the agent space already has an MCP server, setup keeps it.

A space that reuses a server keeps its tool allowlist. If the allowlist misses
tools that this ldcli version allows, setup warns. To add them, pass
--add-mcp-tools. AWS does not send back the classification of each tool, so
the rewrite gives every tool the ldcli classification.

The experiment-orchestration skill tells the agent how to run an experiment
from a goal to a report. Every agent type in the space can load it. If the
skill has the text of an older ldcli version, setup updates it. If someone
edited it, setup does not change it. To overwrite the edits, pass
--replace-skill. The skill is optional: if its step fails, setup warns and
does the other steps.`,
		Args:   cobra.NoArgs,
		PreRun: trackRun(analyticsTrackerFn),
		RunE:   runSetup,
	}

	cmd.Flags().String(regionFlag, "", "AWS region to provision in. Defaults to the region of the current AWS session")
	cmd.Flags().String(profileFlag, "", "Named AWS profile to use. Overrides AWS_PROFILE for this command")
	cmd.Flags().String(agentSpaceIDFlag, "", "Use this agent space. --full-setup runs full setup on a space that setup did not make")
	cmd.Flags().String(agentSpaceNameFlag, "", "Name of the agent space to use or create. In a terminal, setup asks for a name if it needs one")
	cmd.Flags().Bool(newAgentSpaceFlag, false, "Provision a new agent space instead of asking which existing one to use")
	cmd.MarkFlagsMutuallyExclusive(agentSpaceIDFlag, newAgentSpaceFlag)
	cmd.Flags().Bool(fullSetupFlag, false, "Run the full setup on an agent space that setup did not make. Setup can change its IAM roles, account association, and operator app sign-in")
	cmd.Flags().String(authFlowFlag, "iam", "Operator app sign-in method: iam, idc or idp")
	cmd.Flags().String(idcInstanceARNFlag, "", "IAM Identity Center instance ARN, required when --auth-flow=idc")
	cmd.Flags().String(issuerURLFlag, "", "OIDC issuer URL, required when --auth-flow=idp")
	cmd.Flags().String(idpClientIDFlag, "", "OIDC client ID, required when --auth-flow=idp")
	cmd.Flags().String(idpClientSecretFlag, "", "OIDC client secret, required when --auth-flow=idp")
	cmd.Flags().String(mcpServiceIDFlag, "", "Registered MCP server to connect, instead of the server of the AWS account")
	cmd.Flags().String(mcpEndpointFlag, "", "MCP endpoint to register. Setup reuses only a server at this endpoint. Defaults to "+awsdevops.MCPServerEndpoint)
	cmd.Flags().Bool(replaceSkillFlag, false, "Overwrite the "+awsdevops.ExperimentSkillName+" skill if someone edited it")
	cmd.Flags().Bool(addMCPToolsFlag, false, "Add the ldcli tools that the MCP server allowlist of an existing agent space misses. "+
		"Every tool gets the ldcli classification")

	cmd.Flags().Bool(yesFlag, false, "Ask no questions and accept every step")

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runSetup(cmd *cobra.Command, args []string) error {
	opts, err := setupOptions(cmd)
	if err != nil {
		return err
	}

	clients, err := loadClients(cmd)
	if err != nil {
		return err
	}

	plaintext := cliflags.GetOutputKind(cmd) != "json"
	if warning := awsdevops.CheckAWSCLIVersion(cmd.Context()); warning != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
	}
	// The prompts go to stderr, so a json run in a terminal can ask too.
	return setupWith(cmd, clients, opts, plaintext, canPrompt())
}

// setupWith runs setup on clients it is given.
func setupWith(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts awsdevops.SetupOptions,
	plaintext, interactive bool,
) error {
	// Progress goes to stderr, so a json run shows it too.
	opts.Logf = func(format string, args ...any) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", args...)
	}

	// One reader, so no prompt loses input.
	sharedStdin(cmd)

	ask := interactive && !mustBool(cmd, yesFlag)
	provision, err := resolveAgentSpace(cmd, clients, &opts, ask)
	if err != nil {
		return err
	}

	plan, err := awsdevops.PlanSetup(cmd.Context(), clients, opts, provision)
	if err != nil {
		return err
	}
	if !provision && opts.AuthFlowSet {
		// A connect run ignores --auth-flow, so stop and name --full-setup.
		return fmt.Errorf(
			"only a full setup changes the operator app sign-in. To use --%s on agent space %s (%s), also pass --%s. "+
				"To only connect the MCP server, remove --%s",
			authFlowFlag, plan.Space.Name, plan.Space.ID, fullSetupFlag, authFlowFlag,
		)
	}
	// The plan goes to stderr, so a json run shows it too.
	if err := confirmSetup(cmd, plan, ask); err != nil {
		return err
	}

	var result awsdevops.SetupResult
	if provision {
		result, err = awsdevops.RunPlan(cmd.Context(), clients, opts, plan)
	} else {
		result, err = awsdevops.ConnectPlan(cmd.Context(), clients, opts, plan)
	}
	if err != nil && plan.Space.Action == awsdevops.SpaceCreate && result.AgentSpaceID != "" {
		// Name the new space, so a failed CI run says what it made. A run
		// with the same flags can stop on the name, so give the ID.
		return fmt.Errorf("%w. Setup created agent space %s (%s). To use it, run setup again with --%s %s",
			err, plan.Space.Name, result.AgentSpaceID, agentSpaceIDFlag, result.AgentSpaceID)
	}
	if err != nil {
		return err
	}

	if !plaintext {
		// stdout holds only the JSON, so a CI log shows the warning on stderr.
		if result.MCPToolsError != "" {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", result.MCPToolsError)
		}
		if result.SkillError != "" {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", result.SkillError)
		}

		return printJSON(cmd, result)
	}

	if result.MCPToolsError != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nWarning: %s\n", result.MCPToolsError)
	}
	if result.SkillError != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nWarning: %s\n", result.SkillError)
	}

	// Only a connect run leaves the app off, on a space that setup did not
	// make. The full setup would change that space, so point to the console.
	if !result.OperatorAppEnabled {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"\nThe operator app of agent space %s is off. To turn it on, use the AWS console.\n",
			result.AgentSpaceID)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nOpen the DevOps Agent at:\n  %s\n", result.OperatorAppURL)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Manage it in the AWS console at:\n  %s\n", awsdevops.ConsoleURL(result.Region))

	return nil
}

// resolveAgentSpace picks the agent space for this run. If no flag picks one,
// it asks in a terminal. A space that setup made gets the full setup. Another
// space gets only the MCP server, unless --full-setup is passed. It returns true
// if setup must provision a new space, and then sets opts.AgentSpaceName.
// Otherwise, it sets opts.AgentSpaceID.
func resolveAgentSpace(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts *awsdevops.SetupOptions,
	interactive bool,
) (bool, error) {
	// The flags conflict, so at most one of these is set.
	if opts.AgentSpaceID != "" {
		// Read only this space, so the other spaces cannot stop the run.
		space, err := awsdevops.AgentSpace(cmd.Context(), clients, opts.AgentSpaceID)
		var missing *agenttypes.ResourceNotFoundException
		switch {
		case errors.As(err, &missing):
			return false, fmt.Errorf(
				"no agent space %s in %s. Check --%s and --%s, or run setup without --%s to list the agent spaces",
				opts.AgentSpaceID, clients.Region, regionFlag, profileFlag, agentSpaceIDFlag,
			)
		case err != nil:
			return false, err
		case space.TagErr != nil && !opts.FullSetup:
			// The owner is unknown, so do only what is safe on any space.
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"Setup could not read the tags of agent space %s (%s), so it only connects the MCP server. "+
					"To run the full setup on it, pass --%s: %v\n",
				space.Name, space.AgentSpaceID, fullSetupFlag, space.TagErr)

			return false, nil
		}

		return fullSetupOn(cmd, *opts, space), nil
	}
	if opts.NewAgentSpace {
		// A new space needs only a free name, so do not read the tags. A
		// failed tag lookup must not stop a run that only creates a space.
		spaces, err := awsdevops.AgentSpaceNames(cmd.Context(), clients)
		if err != nil {
			return false, err
		}

		return true, nameNewSpace(cmd, spaces, opts, interactive)
	}
	spaces, err := awsdevops.AgentSpaces(cmd.Context(), clients)
	if err != nil {
		return false, err
	}
	if len(spaces) == 0 {
		opts.NewAgentSpace = true

		return true, nameNewSpace(cmd, spaces, opts, interactive)
	}

	if !interactive {
		provision, err := pickWithoutAsking(spaces, opts, clients.Region)
		if err == nil && opts.AgentSpaceID != "" {
			warnFullSetup(cmd, *opts, summaryOf(spaces, opts.AgentSpaceID))
		}

		return provision, err
	}

	choice, err := selectAgentSpace(cmd, spaces, clients.Region, opts.AgentSpaceName)
	if err != nil {
		return false, err
	}
	if choice.newSpace {
		opts.NewAgentSpace = true
		opts.AgentSpaceName = choice.name

		return true, nameNewSpace(cmd, spaces, opts, interactive)
	}
	opts.AgentSpaceID = choice.id

	return fullSetupOn(cmd, *opts, summaryOf(spaces, choice.id)), nil
}

// fullSetupOn reports whether the chosen space gets the full setup. A space
// that setup made gets it. --full-setup forces it on another space, with a
// warning.
func fullSetupOn(cmd *cobra.Command, opts awsdevops.SetupOptions, space awsdevops.AgentSpaceSummary) bool {
	warnFullSetup(cmd, opts, space)

	return opts.FullSetup || space.ManagedByLDCLI
}

// warnFullSetup warns that --full-setup runs the full setup on a space that
// setup did not make, so that a wrong --agent-space-id shows before the plan.
func warnFullSetup(cmd *cobra.Command, opts awsdevops.SetupOptions, space awsdevops.AgentSpaceSummary) {
	if !opts.FullSetup || space.ManagedByLDCLI {
		return
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"Warning: setup did not make agent space %s (%s). --%s runs the full setup on it, "+
			"so setup can change its IAM roles, account association, and operator app sign-in.\n",
		space.Name, space.AgentSpaceID, fullSetupFlag)
}

// summaryOf returns the summary of the space with this ID.
func summaryOf(spaces []awsdevops.AgentSpaceSummary, id string) awsdevops.AgentSpaceSummary {
	for _, space := range spaces {
		if space.AgentSpaceID == id {
			return space
		}
	}

	return awsdevops.AgentSpaceSummary{AgentSpaceID: id}
}

// pickWithoutAsking picks a space for a run that cannot ask. With a name, it
// does what Enter does at the list: it uses the one space with that name, or
// creates the space when no space has the name. Without a name, it reuses the
// one space that setup made, so an unattended re-run stays idempotent. It never
// guesses between spaces that setup did not make.
//
// A space whose tags setup could not read has an unknown owner. A run that
// cannot ask stops rather than pick around such a space.
func pickWithoutAsking(all []awsdevops.AgentSpaceSummary, opts *awsdevops.SetupOptions, region string) (bool, error) {
	spaces, unread := splitByTag(all)
	name := opts.AgentSpaceName
	if name != "" && hasSpaceNamed(unread, name) {
		return false, fmt.Errorf(
			"setup could not read the tags of the agent space named %s in %s (%s); pass --%s to pick a space",
			name, region, strings.Join(agentSpaceLabels(unread), ", "), agentSpaceIDFlag,
		)
	}
	if name == "" && len(unread) > 0 {
		return false, fmt.Errorf(
			"setup could not read the tags of %s in %s (%s); pass --%s or --%s to pick a space",
			spaceCount(len(unread)), region, strings.Join(agentSpaceLabels(unread), ", "),
			agentSpaceIDFlag, agentSpaceNameFlag,
		)
	}
	if name != "" {
		if i := spaceNamed(spaces, name); i >= 0 {
			opts.AgentSpaceID = spaces[i].AgentSpaceID

			return opts.FullSetup || spaces[i].ManagedByLDCLI, nil
		}
		if !hasSpaceNamed(spaces, name) {
			opts.NewAgentSpace = true

			return true, nil
		}

		var named []awsdevops.AgentSpaceSummary
		for _, space := range spaces {
			if strings.EqualFold(space.Name, name) {
				named = append(named, space)
			}
		}

		return false, fmt.Errorf(
			"more than one agent space in %s is named %s (%s); pass --%s to pick one",
			region, name, strings.Join(agentSpaceLabels(named), ", "), agentSpaceIDFlag,
		)
	}

	if own := ownSpace(spaces); own >= 0 {
		opts.AgentSpaceID = spaces[own].AgentSpaceID

		return true, nil
	}

	return false, fmt.Errorf(
		"this AWS account already has agent spaces in %s (%s); pass --%s to connect "+
			"LaunchDarkly to one of them, or --%s to use or create a space by name",
		region,
		strings.Join(agentSpaceLabels(spaces), ", "),
		agentSpaceIDFlag,
		agentSpaceNameFlag,
	)
}

// splitByTag splits the spaces into those with a known owner and those whose
// tags setup could not read.
func splitByTag(spaces []awsdevops.AgentSpaceSummary) (read, unread []awsdevops.AgentSpaceSummary) {
	for _, space := range spaces {
		if space.TagErr != nil {
			unread = append(unread, space)
		} else {
			read = append(read, space)
		}
	}

	return read, unread
}

// notListed starts the note about the spaces that the list leaves out.
func notListed(n int) string {
	if n == 1 {
		return "1 agent space is"
	}

	return fmt.Sprintf("%d agent spaces are", n)
}

// spaceCount names a number of agent spaces.
func spaceCount(n int) string {
	if n == 1 {
		return "1 agent space"
	}

	return fmt.Sprintf("%d agent spaces", n)
}

// nameNewSpace makes sure that a new space has a name, and asks for one in a
// terminal. The check that no space has the name is in PlanSetup.
func nameNewSpace(cmd *cobra.Command, spaces []awsdevops.AgentSpaceSummary, opts *awsdevops.SetupOptions, interactive bool) error {
	if opts.AgentSpaceName != "" {
		return nil
	}
	if !interactive {
		return fmt.Errorf("a new agent space needs a name; pass --%s, for example --%s launchdarkly",
			agentSpaceNameFlag, agentSpaceNameFlag)
	}

	name, err := askSpaceName(cmd, spaces)
	opts.AgentSpaceName = name

	return err
}

// askSpaceName asks until the answer is a name that no space uses.
func askSpaceName(cmd *cobra.Command, spaces []awsdevops.AgentSpaceSummary) (string, error) {
	out := cmd.ErrOrStderr()
	in := sharedStdin(cmd)
	for {
		_, _ = fmt.Fprint(out, "Name for the new agent space: ")
		line, err := nextLine(in)
		if err != nil && line == "" {
			return "", fmt.Errorf("no agent space name was given; pass --%s", agentSpaceNameFlag)
		}

		name := strings.TrimSpace(line)
		switch {
		case name == "":
			_, _ = fmt.Fprintln(out, "Enter a name")
		case hasSpaceNamed(spaces, name):
			_, _ = fmt.Fprintf(out, "An agent space named %s already exists. Enter another name\n", name)
		default:
			return name, nil
		}
	}
}

// ownSpace returns the index of the one space that setup made, or -1 when
// there is no such space or more than one.
func ownSpace(spaces []awsdevops.AgentSpaceSummary) int {
	own := -1
	for i, space := range spaces {
		if !space.ManagedByLDCLI {
			continue
		}
		if own >= 0 {
			return -1
		}
		own = i
	}

	return own
}

// spaceNamed returns the index of the one space with this name, or -1 when
// name is empty or no single space has it.
func spaceNamed(spaces []awsdevops.AgentSpaceSummary, name string) int {
	found := -1
	for i, space := range spaces {
		if name == "" || !strings.EqualFold(space.Name, name) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}

	return found
}

// hasSpaceNamed reports whether any space has this name.
func hasSpaceNamed(spaces []awsdevops.AgentSpaceSummary, name string) bool {
	for _, space := range spaces {
		if strings.EqualFold(space.Name, name) {
			return true
		}
	}

	return false
}

// confirmSetup shows the plan and asks.
func confirmSetup(cmd *cobra.Command, plan awsdevops.SetupPlan, ask bool) error {
	out := cmd.ErrOrStderr()
	_, _ = fmt.Fprintln(out, "\nSetup plan:")
	_, _ = fmt.Fprintf(out, "  AWS account:   %s\n", plan.AccountID)
	_, _ = fmt.Fprintf(out, "  Region:        %s\n", plan.Region)
	_, _ = fmt.Fprintf(out, "  AWS profile:   %s\n", profileLabel(cmd))
	text := plan.Text()
	_, _ = fmt.Fprintf(out, "  Setup:         %s\n", text.Scope)
	for _, role := range text.Roles {
		_, _ = fmt.Fprintf(out, "  IAM role:      %s\n", role)
	}
	_, _ = fmt.Fprintf(out, "  Agent space:   %s\n", text.AgentSpace)
	_, _ = fmt.Fprintf(out, "  Association:   %s\n", text.Association)
	_, _ = fmt.Fprintf(out, "  Operator app:  %s\n", text.OperatorApp)
	_, _ = fmt.Fprintf(out, "  MCP server:    %s\n", text.MCPServer)
	_, _ = fmt.Fprintf(out, "  Skill:         %s\n\n", text.Skill)
	if !ask {
		return nil
	}
	_, _ = fmt.Fprint(out, "Continue? [Y/n]: ")
	if readYes(sharedStdin(cmd)) {
		return nil
	}
	_, _ = fmt.Fprintln(out)

	return errors.New("setup canceled before it changed AWS")
}

// profileLabel names the AWS profile in use.
func profileLabel(cmd *cobra.Command) string {
	if profile, chosen := awsdevops.SelectedProfile(mustString(cmd, profileFlag)); chosen {
		return profile
	}

	return "default credentials"
}

// spaceChoice is an answer at the space list. If newSpace is true, name is
// the name for the new space, or empty when setup must ask for one.
type spaceChoice struct {
	id       string
	newSpace bool
	name     string
}

// selectAgentSpace asks which agent space to connect LaunchDarkly to. With a
// name, the highlight starts on the space with that name, or on a new space
// with that name when no space has it. Without a name, the highlight starts
// on the one space that setup made. If the space is not clear, the highlight
// starts on a new space, so Enter does not connect to a space of another
// owner.
//
// The list leaves out a space whose tags setup could not read, because its
// owner is unknown. A note names each of those spaces. The name checks still
// count them, so setup does not offer a new space with the name of one.
func selectAgentSpace(
	cmd *cobra.Command,
	all []awsdevops.AgentSpaceSummary,
	region string,
	name string,
) (spaceChoice, error) {
	spaces, unread := splitByTag(all)
	choices := make([]spaceChoice, 0, len(spaces)+2)
	labels := make([]string, 0, len(spaces)+2)
	for _, space := range spaces {
		label := fmt.Sprintf("%s (%s)", space.Name, space.AgentSpaceID)
		if space.ManagedByLDCLI {
			label += ", made by ldcli"
		}
		choices = append(choices, spaceChoice{id: space.AgentSpaceID})
		labels = append(labels, label)
	}

	defaultIndex := spaceNamed(spaces, name)
	if name == "" && len(unread) == 0 {
		// The highlight starts where a run that cannot ask picks: the one
		// space that setup made.
		defaultIndex = ownSpace(spaces)
	}
	if hasSpaceNamed(unread, name) {
		defaultIndex = -1
	}
	if name != "" && !hasSpaceNamed(all, name) {
		defaultIndex = len(choices)
		choices = append(choices, spaceChoice{newSpace: true, name: name})
		labels = append(labels, "provision a new agent space named "+name)
	}
	choices = append(choices, spaceChoice{newSpace: true})
	labels = append(labels, "provision a new agent space")
	start := defaultIndex
	if start < 0 {
		start = len(choices) - 1
	}

	out := cmd.ErrOrStderr()
	_, _ = fmt.Fprintln(out)
	if len(unread) > 0 {
		_, _ = fmt.Fprintf(out,
			"%s not listed, because setup could not read the tags: %s. To use one, pass --%s <id>.\n\n",
			notListed(len(unread)), strings.Join(agentSpaceLabels(unread), ", "), agentSpaceIDFlag)
	}

	choice, err := newChooser(cmd)(
		"Which agent space should LaunchDarkly connect to?",
		fmt.Sprintf("This AWS account already runs the DevOps Agent in %s. Type to search.", region),
		labels,
		start,
	)
	if errors.Is(err, errPickCanceled) {
		return spaceChoice{}, fmt.Errorf("no agent space was chosen; pass --%s or --%s", agentSpaceIDFlag, newAgentSpaceFlag)
	}
	if err != nil {
		return spaceChoice{}, err
	}

	return choices[choice], nil
}

func agentSpaceLabels(spaces []awsdevops.AgentSpaceSummary) []string {
	labels := make([]string, 0, len(spaces))
	for _, space := range spaces {
		labels = append(labels, fmt.Sprintf("%s %s", space.Name, space.AgentSpaceID))
	}

	return labels
}

func setupOptions(cmd *cobra.Command) (awsdevops.SetupOptions, error) {
	opts := awsdevops.SetupOptions{
		AgentSpaceID:    mustString(cmd, agentSpaceIDFlag),
		AgentSpaceName:  mustString(cmd, agentSpaceNameFlag),
		AuthFlow:        strings.ToLower(mustString(cmd, authFlowFlag)),
		AuthFlowSet:     cmd.Flags().Changed(authFlowFlag),
		FullSetup:       mustBool(cmd, fullSetupFlag),
		IdcInstanceARN:  mustString(cmd, idcInstanceARNFlag),
		IssuerURL:       mustString(cmd, issuerURLFlag),
		IdpClientID:     mustString(cmd, idpClientIDFlag),
		IdpClientSecret: mustString(cmd, idpClientSecretFlag),
		NewAgentSpace:   mustBool(cmd, newAgentSpaceFlag),
		MCPServiceID:    mustString(cmd, mcpServiceIDFlag),
		MCPEndpoint:     mustString(cmd, mcpEndpointFlag),
		OpenURL:         browser.OpenURL,
		ReplaceSkill:    mustBool(cmd, replaceSkillFlag),
		AddMCPTools:     mustBool(cmd, addMCPToolsFlag),
	}

	idcSet := opts.IdcInstanceARN != ""
	idpSet := opts.IssuerURL != "" || opts.IdpClientID != "" || opts.IdpClientSecret != ""
	if idcSet && opts.AuthFlow != "idc" {
		return opts, fmt.Errorf("--%s needs --%s=idc", idcInstanceARNFlag, authFlowFlag)
	}
	if idpSet && opts.AuthFlow != "idp" {
		return opts, fmt.Errorf("--%s, --%s and --%s need --%s=idp", issuerURLFlag, idpClientIDFlag, idpClientSecretFlag, authFlowFlag)
	}

	switch opts.AuthFlow {
	case "iam":
	case "idc":
		if opts.IdcInstanceARN == "" {
			return opts, fmt.Errorf("--%s is required when --%s=idc", idcInstanceARNFlag, authFlowFlag)
		}
	case "idp":
		if opts.IssuerURL == "" || opts.IdpClientID == "" || opts.IdpClientSecret == "" {
			return opts, fmt.Errorf("--%s, --%s and --%s are required when --%s=idp", issuerURLFlag, idpClientIDFlag, idpClientSecretFlag, authFlowFlag)
		}
	default:
		return opts, fmt.Errorf("--%s must be iam, idc or idp", authFlowFlag)
	}
	return opts, nil
}

// mustString and friends read flags this package declares itself, so a lookup
// error is a programming error rather than user input.
func mustString(cmd *cobra.Command, name string) string {
	value, err := cmd.Flags().GetString(name)
	if err != nil {
		panic(err)
	}

	return value
}

func mustBool(cmd *cobra.Command, name string) bool {
	value, err := cmd.Flags().GetBool(name)
	if err != nil {
		panic(err)
	}

	return value
}

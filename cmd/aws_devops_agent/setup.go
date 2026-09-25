package awsdevopsagent

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const (
	agentSpaceNameFlag       = "agent-space-name"
	newAgentSpaceFlag        = "new-agent-space"
	replaceMCPTokenFlag      = "replace-mcp-token"
	mcpReadOnlyToolsFlag     = "mcp-read-only-tools"
	mcpMutativeToolsFlag     = "mcp-mutative-tools"
	skillFlag                = "skill"
	githubOwnerFlag          = "github-owner"
	githubRepoFlag           = "github-repo"
	githubTargetBranchesFlag = "github-target-branches"
	protectedBranchFlag      = "protected-branch"
	kiroAPIKeyFlag           = "kiro-api-key"
	skipFlag                 = "skip"
	noWaitFlag               = "no-wait"
)

func NewSetupCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Provision the AWS DevOps Agent for LaunchDarkly",
		Long: `Create the IAM roles, agent space, AWS account association, operator app,
LaunchDarkly MCP server connection, GitHub repository association, Kiro agent
workflow files, GitHub Actions permissions, branch protection and the built-in
experiment-orchestration skill.

Re-running is safe: setup reuses the agent space matching --agent-space-name
along with anything already attached to it. Files already on the repository are
left as they are so a hand edit is never overwritten. Steps that AWS only
exposes through the console, such as the GitHub App installation, are listed at
the end of the run.

The MCP server is connected with --access-token, or with the token in your
ldcli configuration when you do not pass one. With neither, setup pauses at the
LaunchDarkly page where you create a service token and uses the token you
paste. AWS keeps that token, so a session token from 'ldcli login' stops
working once it expires; re-run with --access-token --replace-mcp-token to
rotate it.

Pass --skip=step1,step2 to leave out any of: ` + strings.Join(awsdevops.AllSkipSteps, ", ") + `.

In a terminal, setup pauses on each browser step with the page to open and
resumes once you are done. Pass --no-wait to only list them.`,
		Args:   cobra.NoArgs,
		PreRun: trackRun(analyticsTrackerFn),
		RunE:   runSetup,
	}

	cmd.Flags().String(regionFlag, "", "AWS region to provision in. Defaults to the region of the current AWS session")
	cmd.Flags().String(agentSpaceNameFlag, "launchdarkly", "Name of the agent space to create")
	cmd.Flags().Bool(newAgentSpaceFlag, false, "Create another agent space instead of reusing the one matching --agent-space-name")
	cmd.Flags().Bool(replaceMCPTokenFlag, false, "Re-register the LaunchDarkly MCP server so it uses the token passed with --access-token")
	cmd.Flags().StringSlice(mcpReadOnlyToolsFlag, awsdevops.DefaultMCPReadOnlyTools, "LaunchDarkly MCP tools the agent may call without approval")
	cmd.Flags().StringSlice(mcpMutativeToolsFlag, awsdevops.DefaultMCPMutativeTools, "LaunchDarkly MCP tools that change flag state and need approval")
	cmd.Flags().String(skillFlag, "", "Path to a SKILL.md to upload as a skill asset. When omitted, the built-in experiment-orchestration skill is used")
	cmd.Flags().String(githubOwnerFlag, "", "GitHub owner of the repository to associate")
	cmd.Flags().String(githubRepoFlag, "", "GitHub repository name to associate")
	cmd.Flags().StringSlice(githubTargetBranchesFlag, []string{"main"}, "Branches release readiness review runs against")
	cmd.Flags().String(protectedBranchFlag, "main", "Branch to protect on the associated repository")
	cmd.Flags().String(kiroAPIKeyFlag, "", "Kiro API key to store as the "+awsdevops.KiroSecretName+" secret on the associated repository")
	cmd.Flags().StringSlice(skipFlag, nil, "Steps to skip: "+strings.Join(awsdevops.AllSkipSteps, ", "))
	cmd.Flags().Bool(noWaitFlag, false, "List the browser-only steps instead of pausing on each one")

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runSetup(cmd *cobra.Command, args []string) error {
	opts, err := setupOptions(cmd)
	if err != nil {
		return err
	}

	clients, err := awsdevops.NewClients(cmd.Context(), mustString(cmd, regionFlag))
	if err != nil {
		return err
	}

	plaintext := cliflags.GetOutputKind(cmd) != "json"
	if warning := awsdevops.CheckAWSCLIVersion(cmd.Context()); warning != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
	}
	if opts.LDAccessToken != "" && accessTokenFromConfig(cmd) {
		_, _ = fmt.Fprintln(
			cmd.ErrOrStderr(),
			"Using the access token from your ldcli configuration for the MCP server. "+
				"Pass --access-token with a service token if that one expires",
		)
	}
	if plaintext {
		opts.Logf = func(format string, args ...any) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", args...)
		}
	}

	result, err := awsdevops.Setup(cmd.Context(), clients, opts)
	if err != nil {
		return err
	}

	if opts.KiroAPIKey != "" && opts.GitHubOwner != "" && opts.GitHubRepo != "" {
		if err := awsdevops.StoreKiroAPIKey(cmd.Context(), opts.GitHubOwner, opts.GitHubRepo, opts.KiroAPIKey); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Stored %s on %s/%s\n", awsdevops.KiroSecretName, opts.GitHubOwner, opts.GitHubRepo)
	}

	if !plaintext {
		return printJSON(cmd, result)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nAgent space %s is ready in %s (account %s).\n", result.AgentSpaceID, result.Region, result.AccountID)
	if result.OperatorAppURL != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Operator app: %s\n", result.OperatorAppURL)
	}
	remaining := result.RemainingManualSteps
	if !mustBool(cmd, noWaitFlag) && canPrompt() {
		remaining = walkManualSteps(cmd, clients, opts, &result)
	}
	if len(remaining) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nSteps AWS cannot automate:")
		for _, step := range remaining {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n    %s\n", step.Description, step.URL)
		}
	}

	return nil
}

func setupOptions(cmd *cobra.Command) (awsdevops.SetupOptions, error) {
	skip := mustStringSlice(cmd, skipFlag)
	if err := validateSkipSteps(skip); err != nil {
		return awsdevops.SetupOptions{}, err
	}

	return awsdevops.SetupOptions{
		AgentSpaceName:       mustString(cmd, agentSpaceNameFlag),
		NewAgentSpace:        mustBool(cmd, newAgentSpaceFlag),
		LDBaseURI:            viper.GetString(cliflags.BaseURIFlag),
		LDAccessToken:        viper.GetString(cliflags.AccessTokenFlag),
		ReplaceMCPToken:      mustBool(cmd, replaceMCPTokenFlag),
		MCPReadOnlyTools:     mustStringSlice(cmd, mcpReadOnlyToolsFlag),
		MCPMutativeTools:     mustStringSlice(cmd, mcpMutativeToolsFlag),
		SkillPath:            mustString(cmd, skillFlag),
		GitHubOwner:          mustString(cmd, githubOwnerFlag),
		GitHubRepo:           mustString(cmd, githubRepoFlag),
		GitHubTargetBranches: mustStringSlice(cmd, githubTargetBranchesFlag),
		ProtectedBranch:      mustString(cmd, protectedBranchFlag),
		KiroAPIKey:           mustString(cmd, kiroAPIKeyFlag),
		Skip:                 skip,
	}, nil
}

// validateSkipSteps rejects unknown --skip values up front so a typo does not
// silently run every step.
func validateSkipSteps(skip []string) error {
	valid := map[string]bool{}
	for _, s := range awsdevops.AllSkipSteps {
		valid[s] = true
	}
	for _, entry := range skip {
		if !valid[strings.ToLower(entry)] {
			return fmt.Errorf("--%s=%q is not a step: valid values are %s",
				skipFlag, entry, strings.Join(awsdevops.AllSkipSteps, ", "))
		}
	}

	return nil
}

// accessTokenFromConfig reports whether the token came from the ldcli
// configuration rather than this run, where `ldcli login` may have written a
// session token that AWS would keep past its expiry.
func accessTokenFromConfig(cmd *cobra.Command) bool {
	return !cmd.Flags().Changed(cliflags.AccessTokenFlag) &&
		os.Getenv("LD_ACCESS_TOKEN") == "" &&
		viper.GetString(cliflags.AccessTokenFlag) != ""
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

func mustStringSlice(cmd *cobra.Command, name string) []string {
	value, err := cmd.Flags().GetStringSlice(name)
	if err != nil {
		panic(err)
	}

	return value
}

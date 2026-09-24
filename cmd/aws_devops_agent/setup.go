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
	agentSpaceNameFlag        = "agent-space-name"
	agentSpaceDescriptionFlag = "agent-space-description"
	newAgentSpaceFlag         = "new-agent-space"
	authFlowFlag              = "auth-flow"
	idcInstanceARNFlag        = "idc-instance-arn"
	issuerURLFlag             = "issuer-url"
	idpClientIDFlag           = "idp-client-id"
	idpClientSecretFlag       = "idp-client-secret"
	skipOperatorAppFlag       = "skip-operator-app"
	skipMCPServerFlag         = "skip-mcp-server"
	replaceMCPTokenFlag       = "replace-mcp-token"
	mcpServiceIDFlag          = "mcp-service-id"
	mcpReadOnlyToolsFlag      = "mcp-read-only-tools"
	mcpMutativeToolsFlag      = "mcp-mutative-tools"
	skillNameFlag             = "skill-name"
	skillFileFlag             = "skill-file"
	customAgentNameFlag       = "custom-agent-name"
	customAgentToolsFlag      = "custom-agent-tools"
	scheduleFlag              = "schedule"
	githubServiceIDFlag       = "github-service-id"
	githubOwnerFlag           = "github-owner"
	githubOwnerTypeFlag       = "github-owner-type"
	githubRepoFlag            = "github-repo"
	githubRepoIDFlag          = "github-repo-id"
	githubTargetBranchesFlag  = "github-target-branches"
	kiroAPIKeyFlag            = "kiro-api-key"
	noWaitFlag                = "no-wait"
)

func NewSetupCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Provision the AWS DevOps Agent for LaunchDarkly",
		Long: `Create the IAM roles, agent space, AWS account association, operator app and
LaunchDarkly MCP server connection the AWS DevOps Agent needs.

Re-running is safe: setup reuses the agent space matching --agent-space-name
along with anything already attached to it. Steps that AWS only exposes through
the console, such as the GitHub App installation, are listed at the end of the
run.

The MCP server is connected with --access-token, or with the token in your
ldcli configuration when you do not pass one. With neither, setup pauses at the
LaunchDarkly page where you create a service token and uses the token you
paste. AWS keeps that token and the agent acts as its account and role, so a
session token from 'ldcli login' stops working once it expires.

In a terminal, setup pauses on each browser step with the page to open and
resumes once you are done. Pass --no-wait to only list them.`,
		Args:   cobra.NoArgs,
		PreRun: trackRun(analyticsTrackerFn),
		RunE:   runSetup,
	}

	cmd.Flags().String(regionFlag, "", "AWS region to provision in. Defaults to the region of the current AWS session")
	cmd.Flags().String(agentSpaceIDFlag, "", "Existing agent space to add to instead of creating one")
	cmd.Flags().String(agentSpaceNameFlag, "launchdarkly", "Name of the agent space to create")
	cmd.Flags().String(agentSpaceDescriptionFlag, "Managed by the LaunchDarkly CLI", "Description of the agent space to create")
	cmd.Flags().Bool(newAgentSpaceFlag, false, "Create another agent space instead of reusing the one matching --agent-space-name")
	cmd.Flags().String(authFlowFlag, "iam", "Operator app sign-in method: iam, idc or idp")
	cmd.Flags().String(idcInstanceARNFlag, "", "IAM Identity Center instance ARN, required when --auth-flow=idc")
	cmd.Flags().String(issuerURLFlag, "", "OIDC issuer URL, required when --auth-flow=idp")
	cmd.Flags().String(idpClientIDFlag, "", "OIDC client ID, required when --auth-flow=idp")
	cmd.Flags().String(idpClientSecretFlag, "", "OIDC client secret, required when --auth-flow=idp")
	cmd.Flags().Bool(skipOperatorAppFlag, false, "Skip the operator app role and web app")
	cmd.Flags().Bool(skipMCPServerFlag, false, "Skip registering the LaunchDarkly MCP server, without listing it as a manual step")
	cmd.Flags().Bool(replaceMCPTokenFlag, false, "Re-register the LaunchDarkly MCP server so it uses the token passed with --access-token")
	cmd.Flags().String(mcpServiceIDFlag, "", "Service ID of an MCP server already registered on the account to associate instead of registering one")
	cmd.Flags().StringSlice(mcpReadOnlyToolsFlag, awsdevops.DefaultMCPReadOnlyTools, "LaunchDarkly MCP tools the agent may call without approval")
	cmd.Flags().StringSlice(mcpMutativeToolsFlag, awsdevops.DefaultMCPMutativeTools, "LaunchDarkly MCP tools that change flag state and need approval")
	cmd.Flags().String(skillNameFlag, "launchdarkly", "Name of the skill asset to create from --skill-file")
	cmd.Flags().String(skillFileFlag, "", "Path to a SKILL.md to upload as a skill asset")
	cmd.Flags().String(customAgentNameFlag, "", "Create a custom agent with this name")
	cmd.Flags().StringSlice(customAgentToolsFlag, nil, "Tools the custom agent may use")
	cmd.Flags().String(scheduleFlag, "", "Cron or rate expression to run the custom agent on, for example 'rate(1 day)'")
	cmd.Flags().String(githubServiceIDFlag, "", "Service ID of a GitHub registration to associate with the agent space")
	cmd.Flags().String(githubOwnerFlag, "", "GitHub owner of the repository to associate")
	cmd.Flags().String(githubOwnerTypeFlag, "organization", "GitHub owner type: organization or user")
	cmd.Flags().String(githubRepoFlag, "", "GitHub repository name to associate")
	cmd.Flags().String(githubRepoIDFlag, "", "GitHub repository ID to associate. Read from the GitHub CLI when omitted")
	cmd.Flags().StringSlice(githubTargetBranchesFlag, []string{"main"}, "Branches release readiness review runs against")
	cmd.Flags().String(kiroAPIKeyFlag, "", "Kiro API key to store as the "+awsdevops.KiroSecretName+" secret on the associated repository")
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

	if opts.KiroAPIKey != "" {
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
	opts := awsdevops.SetupOptions{
		AgentSpaceID:          mustString(cmd, agentSpaceIDFlag),
		AgentSpaceName:        mustString(cmd, agentSpaceNameFlag),
		AgentSpaceDescription: mustString(cmd, agentSpaceDescriptionFlag),
		AuthFlow:              strings.ToLower(mustString(cmd, authFlowFlag)),
		IdcInstanceARN:        mustString(cmd, idcInstanceARNFlag),
		IssuerURL:             mustString(cmd, issuerURLFlag),
		IdpClientID:           mustString(cmd, idpClientIDFlag),
		IdpClientSecret:       mustString(cmd, idpClientSecretFlag),
		NewAgentSpace:         mustBool(cmd, newAgentSpaceFlag),
		SkipOperatorApp:       mustBool(cmd, skipOperatorAppFlag),
		LDBaseURI:             viper.GetString(cliflags.BaseURIFlag),
		LDAccessToken:         viper.GetString(cliflags.AccessTokenFlag),
		SkipMCPServer:         mustBool(cmd, skipMCPServerFlag),
		ReplaceMCPToken:       mustBool(cmd, replaceMCPTokenFlag),
		MCPServiceID:          mustString(cmd, mcpServiceIDFlag),
		MCPReadOnlyTools:      mustStringSlice(cmd, mcpReadOnlyToolsFlag),
		MCPMutativeTools:      mustStringSlice(cmd, mcpMutativeToolsFlag),
		SkillName:             mustString(cmd, skillNameFlag),
		CustomAgentName:       mustString(cmd, customAgentNameFlag),
		CustomAgentTools:      mustStringSlice(cmd, customAgentToolsFlag),
		Schedule:              mustString(cmd, scheduleFlag),
		GitHubServiceID:       mustString(cmd, githubServiceIDFlag),
		GitHubOwner:           mustString(cmd, githubOwnerFlag),
		GitHubOwnerType:       mustString(cmd, githubOwnerTypeFlag),
		GitHubRepo:            mustString(cmd, githubRepoFlag),
		GitHubRepoID:          mustString(cmd, githubRepoIDFlag),
		GitHubTargetBranches:  mustStringSlice(cmd, githubTargetBranchesFlag),
		KiroAPIKey:            mustString(cmd, kiroAPIKeyFlag),
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

	if opts.GitHubServiceID != "" && (opts.GitHubOwner == "" || opts.GitHubRepo == "") {
		return opts, fmt.Errorf("--%s and --%s are required with --%s", githubOwnerFlag, githubRepoFlag, githubServiceIDFlag)
	}

	if opts.GitHubOwner != "" && opts.GitHubRepo != "" && opts.GitHubRepoID == "" {
		repo, err := awsdevops.LookupGitHubRepo(cmd.Context(), opts.GitHubOwner, opts.GitHubRepo)
		if err != nil {
			return opts, err
		}
		opts.GitHubRepoID = repo.ID
		if !cmd.Flags().Changed(githubOwnerTypeFlag) && repo.OwnerType != "" {
			opts.GitHubOwnerType = repo.OwnerType
		}
	}

	if path := mustString(cmd, skillFileFlag); path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return opts, fmt.Errorf("unable to read %s: %w", path, err)
		}
		opts.SkillBody = string(body)
	}

	return opts, nil
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

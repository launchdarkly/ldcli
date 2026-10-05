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
	agentSpaceNameFlag  = "agent-space-name"
	newAgentSpaceFlag   = "new-agent-space"
	authFlowFlag        = "auth-flow"
	idcInstanceARNFlag  = "idc-instance-arn"
	issuerURLFlag       = "issuer-url"
	idpClientIDFlag     = "idp-client-id"
	idpClientSecretFlag = "idp-client-secret"
	replaceMCPTokenFlag = "replace-mcp-token"
)

func NewSetupCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Provision the AWS DevOps Agent for LaunchDarkly",
		Long: `Create the IAM roles, agent space, AWS account association, operator app and
LaunchDarkly MCP server connection the AWS DevOps Agent needs.

Re-running is safe: setup reuses the agent space matching --agent-space-name
along with anything already attached to it. The run ends with the operator app
URL, where the agent is used.

The MCP server is connected with --access-token, or with the token in your
ldcli configuration when you do not pass one. With neither, setup asks for a
LaunchDarkly service token. AWS keeps that token and the agent acts as its
account and role, so a session token from 'ldcli login' stops working once it
expires.`,
		Args:   cobra.NoArgs,
		PreRun: trackRun(analyticsTrackerFn),
		RunE:   runSetup,
	}

	cmd.Flags().String(regionFlag, "", "AWS region to provision in. Defaults to the region of the current AWS session")
	cmd.Flags().String(profileFlag, "", "Named AWS profile to use. Overrides AWS_PROFILE for this command")
	cmd.Flags().String(agentSpaceIDFlag, "", "Existing agent space to add to instead of creating one")
	cmd.Flags().String(agentSpaceNameFlag, "launchdarkly", "Name of the agent space to create")
	cmd.Flags().Bool(newAgentSpaceFlag, false, "Create another agent space instead of reusing the one matching --agent-space-name")
	cmd.Flags().String(authFlowFlag, "iam", "Operator app sign-in method: iam, idc or idp")
	cmd.Flags().String(idcInstanceARNFlag, "", "IAM Identity Center instance ARN, required when --auth-flow=idc")
	cmd.Flags().String(issuerURLFlag, "", "OIDC issuer URL, required when --auth-flow=idp")
	cmd.Flags().String(idpClientIDFlag, "", "OIDC client ID, required when --auth-flow=idp")
	cmd.Flags().String(idpClientSecretFlag, "", "OIDC client secret, required when --auth-flow=idp")
	cmd.Flags().Bool(replaceMCPTokenFlag, false, "Re-register the LaunchDarkly MCP server so it uses the token passed with --access-token")

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runSetup(cmd *cobra.Command, args []string) error {
	opts, err := setupOptions(cmd)
	if err != nil {
		return err
	}

	clients, err := awsdevops.NewClients(cmd.Context(), mustString(cmd, regionFlag), mustString(cmd, profileFlag))
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

	if !plaintext {
		return printJSON(cmd, result)
	}

	var mcpErr error
	if result.MCPServiceID == "" {
		mcpErr = connectMCPServer(cmd, clients, opts, &result)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nOpen the DevOps Agent at:\n  %s\n", result.OperatorAppURL)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Manage it in the AWS console at:\n  %s\n", awsdevops.ConsoleURL(result.Region))

	return mcpErr
}

// connectMCPServer collects a LaunchDarkly service token and registers the MCP
// server with it, for a run that started without a token to register.
func connectMCPServer(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts awsdevops.SetupOptions,
	result *awsdevops.SetupResult,
) error {
	tokenURL := awsdevops.AccessTokenURL(opts.LDBaseURI)
	if !canPrompt() {
		_, _ = fmt.Fprintf(
			cmd.ErrOrStderr(),
			"The LaunchDarkly MCP server is not connected. Create a service token at %s, "+
				"then re-run setup with --access-token\n",
			tokenURL,
		)

		return nil
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nCreate a LaunchDarkly service token at:\n  %s\n\n", tokenURL)
	_, _ = fmt.Fprint(cmd.OutOrStdout(), "Paste the token, or press Enter to skip: ")
	opts.LDAccessToken = readSecret(cmd.InOrStdin())
	_, _ = fmt.Fprintln(cmd.OutOrStdout())
	if opts.LDAccessToken == "" {
		return nil
	}

	return awsdevops.RegisterMCPServer(cmd.Context(), clients, opts, result)
}

func setupOptions(cmd *cobra.Command) (awsdevops.SetupOptions, error) {
	opts := awsdevops.SetupOptions{
		AgentSpaceID:    mustString(cmd, agentSpaceIDFlag),
		AgentSpaceName:  mustString(cmd, agentSpaceNameFlag),
		AuthFlow:        strings.ToLower(mustString(cmd, authFlowFlag)),
		IdcInstanceARN:  mustString(cmd, idcInstanceARNFlag),
		IssuerURL:       mustString(cmd, issuerURLFlag),
		IdpClientID:     mustString(cmd, idpClientIDFlag),
		IdpClientSecret: mustString(cmd, idpClientSecretFlag),
		NewAgentSpace:   mustBool(cmd, newAgentSpaceFlag),
		LDBaseURI:       viper.GetString(cliflags.BaseURIFlag),
		LDAccessToken:   viper.GetString(cliflags.AccessTokenFlag),
		ReplaceMCPToken: mustBool(cmd, replaceMCPTokenFlag),
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

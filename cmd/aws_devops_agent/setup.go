package awsdevopsagent

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
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

When the account already has agent spaces, setup asks which one to connect
LaunchDarkly to and only registers the MCP server on it, leaving the rest of
that space as it is. Pass --agent-space-id to pick one without being asked, or
--new-agent-space to provision another space. The run ends with the operator
app URL, where the agent is used.

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
	cmd.Flags().String(agentSpaceIDFlag, "", "Connect LaunchDarkly to this existing agent space instead of asking which one to use")
	cmd.Flags().String(agentSpaceNameFlag, "launchdarkly", "Name of the agent space to create")
	cmd.Flags().Bool(newAgentSpaceFlag, false, "Provision a new agent space instead of asking which existing one to use")
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

	provision, err := resolveAgentSpace(cmd, clients, &opts, plaintext && canPrompt())
	if err != nil {
		return err
	}

	run := awsdevops.Setup
	if !provision {
		run = awsdevops.ConnectExisting
	}

	result, err := run(cmd.Context(), clients, opts)
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

// resolveAgentSpace decides whether this run provisions an agent space or
// connects LaunchDarkly to one the account already has, asking which one when
// the flags leave the choice open. It reports whether to provision, and sets
// opts.AgentSpaceID when it does not.
func resolveAgentSpace(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts *awsdevops.SetupOptions,
	interactive bool,
) (bool, error) {
	if opts.NewAgentSpace {
		return true, nil
	}
	if opts.AgentSpaceID != "" {
		return false, nil
	}

	spaces, err := awsdevops.AgentSpaces(cmd.Context(), clients)
	if err != nil {
		return false, err
	}
	if len(spaces) == 0 {
		return true, nil
	}

	if !interactive {
		// Reuse the space this command provisions by name, so an unattended
		// re-run stays idempotent, but never guess between spaces someone
		// else set up.
		for _, space := range spaces {
			if strings.EqualFold(space.Name, opts.AgentSpaceName) {
				return true, nil
			}
		}

		return false, fmt.Errorf(
			"this AWS account already has agent spaces in %s (%s); pass --%s to connect "+
				"LaunchDarkly to one of them, or --%s to provision another",
			clients.Region,
			strings.Join(agentSpaceLabels(spaces), ", "),
			agentSpaceIDFlag,
			newAgentSpaceFlag,
		)
	}

	choice, err := selectAgentSpace(cmd, spaces, clients.Region)
	if err != nil {
		return false, err
	}
	if choice == "" {
		opts.NewAgentSpace = true

		return true, nil
	}
	opts.AgentSpaceID = choice

	return false, nil
}

// selectAgentSpace asks which agent space to connect LaunchDarkly to, and
// returns an empty string when the answer is to provision a new one.
func selectAgentSpace(cmd *cobra.Command, spaces []awsdevops.AgentSpaceSummary, region string) (string, error) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "\nThis AWS account already runs the DevOps Agent in %s:\n\n", region)
	for i, space := range spaces {
		_, _ = fmt.Fprintf(out, "  %d) %s (%s)\n", i+1, space.Name, space.AgentSpaceID)
	}
	newSpaceChoice := len(spaces) + 1
	_, _ = fmt.Fprintf(out, "  %d) provision a new agent space\n\n", newSpaceChoice)

	// One reader for the whole prompt, so a retry does not lose input the
	// previous read already buffered.
	reader := bufio.NewReader(cmd.InOrStdin())
	for {
		_, _ = fmt.Fprint(out, "Which one should LaunchDarkly connect to? [1]: ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("no agent space was chosen; pass --%s or --%s", agentSpaceIDFlag, newAgentSpaceFlag)
		}

		answer := strings.TrimSpace(line)
		if answer == "" {
			return spaces[0].AgentSpaceID, nil
		}

		choice, err := strconv.Atoi(answer)
		if err != nil || choice < 1 || choice > newSpaceChoice {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Enter a number between 1 and %d\n", newSpaceChoice)

			continue
		}
		if choice == newSpaceChoice {
			return "", nil
		}

		return spaces[choice-1].AgentSpaceID, nil
	}
}

func agentSpaceLabels(spaces []awsdevops.AgentSpaceSummary) []string {
	labels := make([]string, 0, len(spaces))
	for _, space := range spaces {
		labels = append(labels, fmt.Sprintf("%s %s", space.Name, space.AgentSpaceID))
	}

	return labels
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

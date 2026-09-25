package awsdevopsagent

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const (
	serviceIDFlag        = "service-id"
	deregisterGitHubFlag = "deregister-github"
	deleteRolesFlag      = "delete-roles"
	forceFlag            = "force"
)

func NewTeardownCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Remove the AWS DevOps Agent resources created by setup",
		Long: `Disassociate services, delete assets and agent spaces, deregister services and
delete the IAM roles that setup created.

Without flags this removes everything in the account and region, after asking
for confirmation. The flags narrow it to part of that.`,
		Args:   cobra.NoArgs,
		PreRun: trackRun(analyticsTrackerFn),
		RunE:   runTeardown,
	}

	cmd.Flags().String(regionFlag, "", "AWS region to tear down in. Defaults to the region of the current AWS session")
	cmd.Flags().String(agentSpaceIDFlag, "", "Agent space to empty and delete")
	cmd.Flags().StringSlice(serviceIDFlag, nil, "Registered services to deregister, such as the LaunchDarkly MCP server")
	cmd.Flags().Bool(deregisterGitHubFlag, false, "Also deregister the account's GitHub registration")
	cmd.Flags().Bool(deleteRolesFlag, false, "Also delete the IAM roles setup created")
	cmd.Flags().Bool(forceFlag, false, "Remove everything without asking for confirmation")

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runTeardown(cmd *cobra.Command, args []string) error {
	clients, err := awsdevops.NewClients(cmd.Context(), mustString(cmd, regionFlag))
	if err != nil {
		return err
	}

	opts := awsdevops.TeardownOptions{
		AgentSpaceID:     mustString(cmd, agentSpaceIDFlag),
		ServiceIDs:       mustStringSlice(cmd, serviceIDFlag),
		DeregisterGitHub: mustBool(cmd, deregisterGitHubFlag),
		DeleteRoles:      mustBool(cmd, deleteRolesFlag),
	}
	if opts.RemovesEverything() && !mustBool(cmd, forceFlag) {
		accountID, err := clients.AccountID(cmd.Context())
		if err != nil {
			return err
		}
		if err := confirmTeardown(cmd, accountID, clients.Region); err != nil {
			return err
		}
	}

	opts.Logf = func(format string, args ...any) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", args...)
	}

	return awsdevops.Teardown(cmd.Context(), clients, opts)
}

func confirmTeardown(cmd *cobra.Command, accountID, region string) error {
	if !canPrompt() {
		return errors.New(
			"teardown without flags removes every agent space, registered service and IAM role: pass --force to confirm",
		)
	}

	keys := newKeyReader(cmd.InOrStdin())
	defer keys.close()
	out := keys.writer(cmd.OutOrStdout())
	_, _ = fmt.Fprintf(
		out,
		"This removes every agent space, registered service and IAM role in account %s (%s).\nPress y to continue: ",
		accountID,
		region,
	)
	key := keys.next()
	_, _ = fmt.Fprintln(out)
	if key != 'y' && key != 'Y' {
		return errors.New("teardown cancelled")
	}

	return nil
}

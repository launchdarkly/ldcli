package awsdevopsagent

import (
	"fmt"

	"github.com/spf13/cobra"

	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const (
	serviceIDFlag   = "service-id"
	deleteRolesFlag = "delete-roles"
)

func NewTeardownCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Remove the AWS DevOps Agent resources created by setup",
		Long: `Disassociate services, delete assets and the agent space, deregister services
and optionally delete the IAM roles that setup created.`,
		Args:   cobra.NoArgs,
		PreRun: trackRun(analyticsTrackerFn),
		RunE:   runTeardown,
	}

	cmd.Flags().String(regionFlag, "", "AWS region to tear down in. Defaults to the region of the current AWS session")
	cmd.Flags().String(agentSpaceIDFlag, "", "Agent space to empty and delete")
	cmd.Flags().StringSlice(serviceIDFlag, nil, "Registered services to deregister, such as the LaunchDarkly MCP server")
	cmd.Flags().Bool(deleteRolesFlag, false, "Also delete the IAM roles setup created")

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runTeardown(cmd *cobra.Command, args []string) error {
	clients, err := awsdevops.NewClients(cmd.Context(), mustString(cmd, regionFlag))
	if err != nil {
		return err
	}

	return awsdevops.Teardown(cmd.Context(), clients, awsdevops.TeardownOptions{
		AgentSpaceID: mustString(cmd, agentSpaceIDFlag),
		ServiceIDs:   mustStringSlice(cmd, serviceIDFlag),
		DeleteRoles:  mustBool(cmd, deleteRolesFlag),
		Logf: func(format string, args ...any) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", args...)
		},
	})
}

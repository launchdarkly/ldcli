package dev_server

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const DEV_SERVER = "http://0.0.0.0:8765"

func NewListProjectsCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "lists all projects that have been configured for the dev server",
		RunE:  listProjects(client),
		Short: "list all projects",
		Use:   "list",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func listProjects(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		path := DEV_SERVER + "/dev/projects"
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"GET",
			path,
			"application/json",
			nil,
			nil,
			false,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}
		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewGetProjectCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "get the specified project and its configuration for syncing from the LaunchDarkly Service",
		RunE:  getProject(client),
		Short: "get a project",
		Use:   "get",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	return cmd
}

func getProject(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		path := DEV_SERVER + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"GET",
			path,
			"application/json",
			nil,
			nil,
			false,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}
		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}

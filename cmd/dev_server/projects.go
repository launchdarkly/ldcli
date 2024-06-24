package dev_server

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/output"
)

const DEV_SERVER = "http://0.0.0.0:8765"

func NewListProjectsCmd(client dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "lists all projects that have been configured for the dev server",
		RunE:  listProjects(client),
		Short: "list all projects",
		Use:   "list-projects",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func listProjects(client dev_server.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		path := DEV_SERVER + "/dev/projects"
		res, err := client.MakeRequest(
			"GET",
			path,
			nil,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewGetProjectCmd(client dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "get the specified project and its configuration for syncing from the LaunchDarkly Service",
		RunE:  getProject(client),
		Short: "get a project",
		Use:   "get-project",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	return cmd
}

func getProject(client dev_server.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		path := DEV_SERVER + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeRequest(
			"GET",
			path,
			nil,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}
		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewRemoveProjectCmd(client dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "remove the specified project from the dev server",
		RunE:  deleteProject(client),
		Short: "remove a project",
		Use:   "remove-project",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	return cmd
}

func deleteProject(client dev_server.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		path := DEV_SERVER + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeRequest(
			"DELETE",
			path,
			nil,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewAddProjectCmd(client dev_server.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Add the project to the dev server",
		RunE:  addProject(client),
		Short: "add a project",
		Use:   "add-project",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String("source", "", "environment to copy flag values from")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.Flags().SetAnnotation("source", "required", []string{"true"})
	_ = viper.BindPFlag("source", cmd.Flags().Lookup("source"))

	cmd.Flags().String("context-kind", "", "context kind of the context key to use when evaluating flags in source environment")
	_ = viper.BindPFlag("context-kind", cmd.Flags().Lookup("context-kind"))

	cmd.Flags().String("context-key", "", "context key to use when evaluating flags in source environment")
	_ = viper.BindPFlag("context-key", cmd.Flags().Lookup("context-key"))

	cmd.MarkFlagsRequiredTogether("context-kind", "context-key")

	return cmd
}

type context struct {
	key  string
	kind string
}

type postBody struct {
	sourceEnvironmentKey string
	context              context
}

func addProject(client dev_server.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		body := postBody{sourceEnvironmentKey: viper.GetString("source")}
		if viper.IsSet("context-key") {
			body.context = context{
				key:  viper.GetString("context-key"),
				kind: viper.GetString("context-kind"),
			}
		}

		jsonData, err := json.Marshal(body)
		if err != nil {
			return err
		}

		path := DEV_SERVER + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeRequest(
			"POST",
			path,
			jsonData,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(res))

		return nil
	}
}

package dev_server

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewListProjectsCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "projects",
		Args:    validators.Validate(),
		Long:    "lists all projects that have been configured for the dev server",
		RunE:    listProjects(client),
		Short:   "list all projects",
		Use:     "list-projects",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func listProjects(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		path := getDevServerUrl() + "/dev/projects"
		res, err := client.MakeUnauthenticatedRequest(
			"GET",
			path,
			nil,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprint(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewGetProjectCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "projects",
		Args:    validators.Validate(),
		Long:    "get the specified project and its configuration for syncing from the LaunchDarkly Service",
		RunE:    getProject(client),
		Short:   "get a project",
		Use:     "get-project",
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

		path := getDevServerUrl() + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeUnauthenticatedRequest(
			"GET",
			path,
			nil,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}
		fmt.Fprint(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewSyncProjectCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "projects",
		Args:    validators.Validate(),
		Long:    "sync the specified project and its flag configuration with the LaunchDarkly Service",
		RunE:    syncProject(client),
		Short:   "sync project",
		Use:     "sync-project",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	return cmd
}

func syncProject(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		path := getDevServerUrl() + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeUnauthenticatedRequest(
			"PATCH",
			path,
			// An empty body sent to the patch project endpoint = sync project
			[]byte("{}"),
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}
		fmt.Fprint(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewRemoveProjectCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "projects",
		Args:    validators.Validate(),
		Long:    "remove the specified project from the dev server",
		RunE:    deleteProject(client),
		Short:   "remove a project",
		Use:     "remove-project",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	return cmd
}

func deleteProject(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {

		path := getDevServerUrl() + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeUnauthenticatedRequest(
			"DELETE",
			path,
			nil,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprint(cmd.OutOrStdout(), string(res))

		return nil
	}
}

func NewAddProjectCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "projects",
		Args:    validators.Validate(),
		Long:    "Add the project to the dev server",
		RunE:    addProject(client),
		Short:   "add a project",
		Use:     "add-project",
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

	cmd.Flags().String("context", "", `Stringified JSON representation of your context object ex. {"user": { "email": "youremail@gmail.com", "username": "foo", "key": "bar"}}`)
	_ = viper.BindPFlag("context", cmd.Flags().Lookup("context"))

	return cmd
}

type postBody struct {
	SourceEnvironmentKey string          `json:"sourceEnvironmentKey"`
	Context              json.RawMessage `json:"context,omitempty"`
}

func addProject(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		body := postBody{SourceEnvironmentKey: viper.GetString("source")}
		if viper.IsSet("context") {
			body.Context = json.RawMessage(viper.GetString("context"))
		}

		jsonData, err := json.Marshal(body)
		if err != nil {
			return err
		}

		path := getDevServerUrl() + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeUnauthenticatedRequest(
			"POST",
			path,
			jsonData,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		fmt.Fprint(cmd.OutOrStdout(), string(res))

		return nil
	}
}

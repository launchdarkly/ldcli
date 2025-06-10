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
		project := viper.GetString(cliflags.ProjectFlag)
		path := fmt.Sprintf("%s/dev/projects/%s", getDevServerUrl(), project)
		_, err := client.MakeUnauthenticatedRequest(
			"PATCH",
			path,
			// An empty body sent to the patch project endpoint = sync project
			[]byte("{}"),
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "'%s' project synced successfully\n", project)
		if err != nil {
			return err
		}

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

	cmd.Flags().String(SourceEnvironmentFlag, "", "environment to copy flag values from")
	_ = cmd.MarkFlagRequired(SourceEnvironmentFlag)
	_ = cmd.Flags().SetAnnotation(SourceEnvironmentFlag, "required", []string{"true"})
	_ = viper.BindPFlag(SourceEnvironmentFlag, cmd.Flags().Lookup(SourceEnvironmentFlag))

	cmd.Flags().String(ContextFlag, "", `Stringified JSON representation of your context object ex. {"user": { "email": "youremail@gmail.com", "username": "foo", "key": "bar"}}`)
	_ = viper.BindPFlag(ContextFlag, cmd.Flags().Lookup(ContextFlag))

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

func NewUpdateProjectCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "projects",
		Args:    validators.Validate(),
		Long:    "update the specified project and its flag configuration with the LaunchDarkly Service",
		RunE:    updateProject(client),
		Short:   "update project",
		Use:     "update-project",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(SourceEnvironmentFlag, "", "environment to copy flag values from")
	_ = viper.BindPFlag(SourceEnvironmentFlag, cmd.Flags().Lookup(SourceEnvironmentFlag))

	cmd.Flags().String(ContextFlag, "", `Stringified JSON representation of your context object ex. {"user": { "email": "test@gmail.com", "username": "foo", "key": "bar"}}`)
	_ = viper.BindPFlag(ContextFlag, cmd.Flags().Lookup(ContextFlag))

	return cmd
}

type patchBody struct {
	SourceEnvironmentKey string                 `json:"sourceEnvironmentKey,omitempty"`
	Context              map[string]interface{} `json:"context,omitempty"`
}

type patchResponse struct {
	SourceEnvironmentKey string                 `json:"sourceEnvironmentKey"`
	Context              map[string]interface{} `json:"context"`
}

func updateProject(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		body := patchBody{}
		if viper.IsSet(ContextFlag) {
			contextString := viper.GetString(ContextFlag)
			err := json.Unmarshal([]byte(contextString), &body.Context)
			if err != nil {
				return err
			}
		}

		if viper.IsSet(SourceEnvironmentFlag) {
			body.SourceEnvironmentKey = viper.GetString(SourceEnvironmentFlag)
		}

		jsonData, err := json.Marshal(body)
		if err != nil {
			return err
		}

		path := getDevServerUrl() + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag)
		res, err := client.MakeUnauthenticatedRequest(
			"PATCH",
			path,
			jsonData,
		)
		if err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		var response patchResponse
		err = json.Unmarshal(res, &response)
		if err != nil {
			return err
		}

		switch true {
		case !viper.IsSet(ContextFlag) && !viper.IsSet(SourceEnvironmentFlag):
			fmt.Fprint(cmd.OutOrStdout(), "No input given, project synced successfully\n")
		case viper.IsSet(SourceEnvironmentFlag):
			fmt.Fprintf(cmd.OutOrStdout(), "Source environment updated successfully to '%s'\n", response.SourceEnvironmentKey)
			fallthrough
		case viper.IsSet(ContextFlag):
			// pretty print context
			context, err := json.MarshalIndent(response.Context, "", "  ")
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Context updated successfully to:\n%s\n", string(context))
		default:
			fmt.Fprint(cmd.OutOrStdout(), "Project updated successfully\n")
		}

		return nil
	}
}

func NewCopyProjectCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		GroupID: "projects",
		Args:    validators.Validate(),
		Long:    "Copy an existing project to create a new project namespace with the same flags and configuration",
		RunE:    copyProject(client),
		Short:   "copy a project",
		Use:     "copy-project",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().String(cliflags.ProjectFlag, "", "The source project key to copy from")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String("new-project-key", "", "The key for the new copied project")
	_ = cmd.MarkFlagRequired("new-project-key")
	_ = cmd.Flags().SetAnnotation("new-project-key", "required", []string{"true"})
	_ = viper.BindPFlag("new-project-key", cmd.Flags().Lookup("new-project-key"))

	return cmd
}

type copyProjectBody struct {
	NewProjectKey string          `json:"newProjectKey"`
	Context       json.RawMessage `json:"context,omitempty"`
}

func copyProject(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		body := copyProjectBody{NewProjectKey: viper.GetString("new-project-key")}

		jsonData, err := json.Marshal(body)
		if err != nil {
			return err
		}

		path := getDevServerUrl() + "/dev/projects/" + viper.GetString(cliflags.ProjectFlag) + "/copy"
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

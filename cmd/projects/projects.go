package projects

import (
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/internal/analytics"
	"ldcli/internal/projects"
)

func NewProjectsCmd(analyticsTracker analytics.Tracker, client projects.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Make requests (list, create, etc.) on projects",
		Long:  "Make requests (list, create, etc.) on projects",
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}
	listCmd := NewListCmd(client)

	cmd.AddCommand(createCmd)
	cmd.AddCommand(listCmd)

	cmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		analyticsTracker.SendEvent(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			"CLI Command Run",
			buildPropertiesForCommandRunEvent(cmd),
		)
	}

	return cmd, nil
}

func buildPropertiesForCommandRunEvent(cmd *cobra.Command) map[string]interface{} {
	id := uuid.New()
	baseURI := viper.GetString(cliflags.BaseURIFlag)
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, f.Name)
	})

	properties := map[string]interface{}{
		"name":   "projects",
		"action": cmd.CalledAs(),
		"flags":  flags,
		"id":     id.String(),
	}
	if baseURI != cliflags.BaseURIDefault {
		properties["baseURI"] = baseURI
	}
	return properties
}

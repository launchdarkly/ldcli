package environments

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/analytics"
	"ldcli/internal/environments"
)

func NewGetCmd(
	analyticsTracker analytics.Tracker,
	client environments.Client,
) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Return an environment",
		RunE:  runGet(analyticsTracker, client),
		Short: "Return an environment",
		Use:   "get",
	}

	cmd.Flags().StringP(cliflags.EnvironmentFlag, "e", "", "Environment key")
	err := cmd.MarkFlagRequired(cliflags.EnvironmentFlag)
	if err != nil {
		return nil, err
	}

	err = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))
	if err != nil {
		return nil, err
	}

	cmd.Flags().StringP(cliflags.ProjectFlag, "p", "", "Project key")
	err = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func runGet(
	analyticsTracker analytics.Tracker,
	client environments.Client,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))
		_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

		response, err := client.Get(
			context.Background(),
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetString(cliflags.EnvironmentFlag),
			viper.GetString(cliflags.ProjectFlag),
		)
		if err != nil {
			return err
		}

		id := uuid.New()
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		properties := map[string]interface{}{
			"name":   "environment",
			"action": "get",
			"flags":  []string{cliflags.EnvironmentFlag, cliflags.ProjectFlag},
			"id":     id.String(),
		}
		if baseURI != "https://app.launchdarkly.com" {
			properties["baseURI"] = baseURI
		}

		analyticsTracker.SendEvent(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			"CLI Command Run",
			properties,
		)

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}

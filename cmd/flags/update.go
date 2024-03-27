package flags

import (
	"context"
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/validators"
	"ldcli/internal/flags"
)

func NewUpdateCmd(client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Update a flag",
		RunE:  runUpdate(client),
		Short: "Update a flag",
		Use:   "update",
	}

	var data string
	cmd.Flags().StringVarP(
		&data,
		"data",
		"d",
		"",
		"Input data in JSON",
	)
	err := cmd.MarkFlagRequired("data")
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag("data", cmd.Flags().Lookup("data"))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String("key", "", "Flag key")
	err = cmd.MarkFlagRequired("key")
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag("key", cmd.Flags().Lookup("key"))
	if err != nil {
		return nil, err
	}

	cmd.Flags().String("projKey", "", "Project key")
	err = cmd.MarkFlagRequired("projKey")
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag("projKey", cmd.Flags().Lookup("projKey"))
	if err != nil {
		return nil, err
	}

	return cmd, nil
}

func runUpdate(client flags.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// rebind flags used in other subcommands
		_ = viper.BindPFlag("data", cmd.Flags().Lookup("data"))
		_ = viper.BindPFlag("projKey", cmd.Flags().Lookup("projKey"))

		var patch []ldapi.PatchOperation
		err := json.Unmarshal([]byte(viper.GetString("data")), &patch)
		if err != nil {
			return err
		}

		response, err := client.Update(
			context.Background(),
			viper.GetString("api-token"),
			viper.GetString("base-uri"),
			viper.GetString("key"),
			viper.GetString("projKey"),
			patch,
		)
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

		return nil
	}
}

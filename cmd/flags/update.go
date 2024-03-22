package flags

import (
	"context"
	"encoding/json"
	"fmt"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/internal/flags"
)

func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "update",
		Short:   "Update a flag",
		Long:    "Update a flag",
		PreRunE: validate,
		RunE:    runUpdate,
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
		panic(err)
	}
	err = viper.BindPFlag("data", cmd.Flags().Lookup("data"))
	if err != nil {
		panic(err)
	}

	cmd.Flags().String("key", "", "Flag key")
	err = cmd.MarkFlagRequired("key")
	if err != nil {
		panic(err)
	}
	err = viper.BindPFlag("key", cmd.Flags().Lookup("key"))
	if err != nil {
		panic(err)
	}

	cmd.Flags().String("projKey", "", "Project key")
	err = cmd.MarkFlagRequired("projKey")
	if err != nil {
		panic(err)
	}
	err = viper.BindPFlag("projKey", cmd.Flags().Lookup("projKey"))
	if err != nil {
		panic(err)
	}

	return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
	client := flags.NewClient(
		viper.GetString("accessToken"),
		viper.GetString("baseUri"),
	)

	var patch []ldapi.PatchOperation
	// err := json.Unmarshal([]byte(viper.GetString("data")), &patch)
	err := json.Unmarshal([]byte(cmd.Flags().Lookup("data").Value.String()), &patch)
	if err != nil {
		return err
	}

	response, err := client.Update(
		context.Background(),
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

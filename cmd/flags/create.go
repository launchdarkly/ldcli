package flags

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/internal/errors"
	"ldcli/internal/flags"
)

func NewCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a new flag",
		Long:    "Create a new flag",
		PreRunE: validate,
		RunE:    runCreate,
	}

	cmd.Flags().StringP("data", "d", "", "Input data in JSON")
	err := cmd.MarkFlagRequired("data")
	if err != nil {
		panic(err)
	}
	err = viper.BindPFlag("data", cmd.Flags().Lookup("data"))
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

type inputData struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

func runCreate(cmd *cobra.Command, args []string) error {
	client := flags.NewClient(
		viper.GetString("accessToken"),
		viper.GetString("baseUri"),
	)

	// rebind flag to this subcommand
	viper.BindPFlag("projKey", cmd.Flags().Lookup("projKey"))

	var data inputData
	err := json.Unmarshal([]byte(cmd.Flags().Lookup("data").Value.String()), &data)
	// err := json.Unmarshal([]byte(viper.GetString("data")), &data)
	if err != nil {
		return err
	}
	projKey := viper.GetString("projKey")

	response, err := client.Create(
		context.Background(),
		data.Name,
		data.Key,
		projKey,
	)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), string(response)+"\n")

	return nil
}

// validate ensures the flags are valid before using them.
// TODO: refactor with projects validate().
func validate(cmd *cobra.Command, args []string) error {
	_, err := url.ParseRequestURI(viper.GetString("baseUri"))
	if err != nil {
		return errors.ErrInvalidBaseURI
	}

	return nil
}

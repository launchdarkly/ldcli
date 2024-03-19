package projects

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a new project",
		Long:    "Create a new project",
		PreRunE: validate,
		RunE:    runCreate,
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

	return cmd
}

func runCreate(cmd *cobra.Command, args []string) error {
	return nil
}

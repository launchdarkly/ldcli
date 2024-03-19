package projects

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type createCmd struct {
	Cmd *cobra.Command
}

func NewCreateCmd() createCmd {
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

	return createCmd{
		Cmd: cmd,
	}
}

// runCreate creates a new project.
func runCreate(cmd *cobra.Command, args []string) error {
	return nil
}

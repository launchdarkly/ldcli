package flags

import "github.com/spf13/cobra"

func NewFlagsCmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "flags",
		Short: "Make requests (list, create, etc.) on flags",
		Long:  "Make requests (list, create, etc.) on flags",
	}

	updateCmd, err := NewUpdateCmd()
	if err != nil {
		return nil, err
	}
	createCmd, err := NewCreateCmd()
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(updateCmd)
	cmd.AddCommand(createCmd)

	return cmd, nil
}

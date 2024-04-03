package flags

import (
	"github.com/spf13/cobra"

	"ldcli/internal/flags"
)

func NewFlagsCmd(client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "flags",
		Short: "Make requests (list, create, etc.) on flags",
		Long:  "Make requests (list, create, etc.) on flags",
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}
	updateCmd, err := NewUpdateCmd(client)
	if err != nil {
		return nil, err
	}

	toggleOnUpdateCmd, err := NewToggleOnUpdateCmd(client)
	if err != nil {
		return nil, err
	}
	toggleOffUpdateCmd, err := NewToggleOffUpdateCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(toggleOnUpdateCmd)
	cmd.AddCommand(toggleOffUpdateCmd)

	return cmd, nil
}

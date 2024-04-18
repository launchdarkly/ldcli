package flags

import (
	"github.com/spf13/cobra"

	"ldcli/internal/analytics"
	"ldcli/internal/flags"
)

func NewFlagsCmd(analyticsTracker analytics.Tracker, client flags.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "flags",
		Short: "Make requests (list, create, etc.) on flags",
		Long:  "Make requests (list, create, etc.) on flags",
	}

	createCmd, err := NewCreateCmd(analyticsTracker, client)
	if err != nil {
		return nil, err
	}
	getCmd, err := NewGetCmd(analyticsTracker, client)
	if err != nil {
		return nil, err
	}
	updateCmd, err := NewUpdateCmd(analyticsTracker, client)
	if err != nil {
		return nil, err
	}

	toggleOnUpdateCmd, err := NewToggleOnUpdateCmd(analyticsTracker, client)
	if err != nil {
		return nil, err
	}
	toggleOffUpdateCmd, err := NewToggleOffUpdateCmd(analyticsTracker, client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(getCmd)
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(toggleOnUpdateCmd)
	cmd.AddCommand(toggleOffUpdateCmd)

	return cmd, nil
}

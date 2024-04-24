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
		PersistentPreRun: func(c *cobra.Command, args []string) {
			analytics.SendCommandRunEvent("flags", c, analyticsTracker)
		},
	}

	createCmd, err := NewCreateCmd(client)
	if err != nil {
		return nil, err
	}
	getCmd, err := NewGetCmd(client)
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

	for _, c := range cmd.Commands() {
		c.SetHelpFunc(func(c *cobra.Command, args []string) {
			analytics.SendCommandRunEvent("flags", c, analyticsTracker)
			c.Root().Annotations = map[string]string{"help": "true"}
		})
	}

	cmd.AddCommand(createCmd)
	cmd.AddCommand(getCmd)
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(toggleOnUpdateCmd)
	cmd.AddCommand(toggleOffUpdateCmd)

	return cmd, nil
}

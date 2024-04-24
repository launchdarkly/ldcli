package environments

import (
	"github.com/spf13/cobra"

	"ldcli/internal/analytics"
	"ldcli/internal/environments"
)

func NewEnvironmentsCmd(
	analyticsTracker analytics.Tracker,
	client environments.Client,
) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "environments",
		Short: "Make requests (list, create, etc.) on environments",
		Long:  "Make requests (list, create, etc.) on environments",
		PersistentPreRun: func(c *cobra.Command, args []string) {
			analytics.SendCommandRunEvent("environments", c, analyticsTracker)
		},
	}

	getCmd, err := NewGetCmd(client)
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(getCmd)

	return cmd, nil
}

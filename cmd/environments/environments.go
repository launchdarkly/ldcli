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

	for _, c := range cmd.Commands() {
		c.SetHelpFunc(func(c *cobra.Command, args []string) {
			analytics.SendCommandRunEvent("environments", c, analyticsTracker)
			c.Root().Annotations = map[string]string{"help": "true"}
		})
	}

	return cmd, nil
}

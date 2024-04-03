package environments

import (
	"github.com/spf13/cobra"

	"ldcli/internal/environments"
)

func NewEnvironmentsCmd(client environments.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "environments",
		Short: "Make requests (list, create, etc.) on environments",
		Long:  "Make requests (list, create, etc.) on environments",
	}

	getCmd, err := NewGetCmd(client)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(getCmd)

	return cmd, nil
}

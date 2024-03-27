package environments

import (
	"github.com/spf13/cobra"
)

func NewEnvironmentsCmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "environments",
		Short: "Make requests on environments",
		Long:  "Make requests (list, create, etc.) on environments",
	}

	return cmd, nil
}

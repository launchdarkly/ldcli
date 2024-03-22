package flags

import (
	"github.com/spf13/cobra"
)

func NewFlagsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flags",
		Short: "Make requests (list, create, etc.) on flags",
		Long:  "Make requests (list, create, etc.) on flags",
	}

	cmd.AddCommand(NewUpdateCmd())

	return cmd
}

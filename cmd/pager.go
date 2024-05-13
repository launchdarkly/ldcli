package cmd

import (
	"github.com/spf13/cobra"
	"ldcli/cmd/validators"
)

func NewPagerCmd() *cobra.Command {
	return &cobra.Command{
		Args: validators.Validate(),
		Use:  "pager",
	}
}

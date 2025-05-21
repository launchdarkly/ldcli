package sourcemaps

import (
	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/internal/resources"
)

func NewSourcemapsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sourcemaps",
		Short: "Manage sourcemaps",
		Long:  "Manage sourcemaps for LaunchDarkly error monitoring",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(NewUploadCmd(resources.NewClient("")))

	return cmd
}

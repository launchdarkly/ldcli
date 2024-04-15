package config

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	ListFlag  = "list"
	SetFlag   = "set"
	UnsetFlag = "unset"
)

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Long:  "View and modify specific configuration values",
		RunE:  run(),
		Short: "View and modify specific configuration values",
		Use:   "config",
	}

	cmd.Flags().Bool(ListFlag, false, "List configs")
	_ = viper.BindPFlag(ListFlag, cmd.Flags().Lookup(ListFlag))
	cmd.Flags().Bool(SetFlag, false, "Set a config field to a value")
	_ = viper.BindPFlag(SetFlag, cmd.Flags().Lookup(SetFlag))
	cmd.Flags().String(UnsetFlag, "", "Unset a config field")
	_ = viper.BindPFlag(UnsetFlag, cmd.Flags().Lookup(UnsetFlag))

	// TODO: running config should show help

	return cmd
}

func run() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		switch {
		case viper.GetBool(ListFlag):
			fmt.Fprintln(cmd.OutOrStdout(), "called --list flag")
		case viper.GetBool(SetFlag):
			fmt.Fprintln(cmd.OutOrStdout(), "called --set flag")
		case viper.GetBool(UnsetFlag):
			fmt.Fprintln(cmd.OutOrStdout(), "called --unset flag")
		}

		return nil
	}
}

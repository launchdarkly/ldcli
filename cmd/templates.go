package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

func getUsageTemplate() string {
	return `Usage:
  {{.CommandPath}} [command]

Commands:
  {{rpad "setup" 29}} Create your first feature flag using a step-by-step guide
  {{rpad "config" 29}} View and modify specific configuration values
  {{rpad "completion" 29}} Enable command autocompletion within supported shells

Common resource commands:
  {{rpad "flags" 29}} List, create, and modify feature flags and their targeting
  {{rpad "environments" 29}} List, create, and manage environments
  {{rpad "projects" 29}} List, create, and manage projects
  {{rpad "members" 29}} Invite new members to an account
  {{rpad "segments" 29}} List, create, modify, and delete segments
  {{rpad "..." 29}} To see more resource commands, run 'ldcli resources help'

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
`
}

func HasRequiredFlags(cmd *cobra.Command) bool {
	var numFlags int
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		_, ok := flag.Annotations["required"]
		if ok {
			numFlags += 1
		}
	})

	return numFlags > 0
}

func HasOptionalFlags(cmd *cobra.Command) bool {
	var numFlags int
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		_, ok := flag.Annotations["required"]
		if !ok && flag.Name != "help" {
			numFlags += 1
		}
	})

	return numFlags > 0
}

func WrappedRequiredFlagUsages(cmd *cobra.Command) string {
	flagSet := pflag.NewFlagSet("request", pflag.ExitOnError)

	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if _, ok := flag.Annotations["required"]; ok {
			flagSet.AddFlag(flag)
		}
	})

	return flagSet.FlagUsagesWrapped(getTerminalWidth())
}

func WrappedOptionalFlagUsages(cmd *cobra.Command) string {
	flagSet := pflag.NewFlagSet("request", pflag.ExitOnError)

	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		_, ok := flag.Annotations["required"]
		if !ok && flag.Name != "help" {
			flagSet.AddFlag(flag)
		}
	})

	return flagSet.FlagUsagesWrapped(getTerminalWidth())
}

func getTerminalWidth() int {
	var width int

	width, _, err := term.GetSize(0)
	if err != nil {
		width = 80
	}

	return width
}

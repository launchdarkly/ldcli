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
  {{rpad "completion" 29}} Generate the autocompletion script for the specified shell

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

func WrappedRequiredFlagUsages(cmd *cobra.Command) string {
	nonRequestParamsFlags := pflag.NewFlagSet("request", pflag.ExitOnError)

	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if _, ok := flag.Annotations["required"]; ok {
			nonRequestParamsFlags.AddFlag(flag)
		}
	})

	return nonRequestParamsFlags.FlagUsagesWrapped(getTerminalWidth())
}

func WrappedOptionalFlagUsages(cmd *cobra.Command) string {
	nonRequestParamsFlags := pflag.NewFlagSet("request", pflag.ExitOnError)

	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		_, ok := flag.Annotations["required"]
		if !ok && flag.Name != "help" {
			nonRequestParamsFlags.AddFlag(flag)
		}
	})

	return nonRequestParamsFlags.FlagUsagesWrapped(getTerminalWidth())
}

func getTerminalWidth() int {
	var width int

	width, _, err := term.GetSize(0)
	if err != nil {
		width = 80
	}

	return width
}

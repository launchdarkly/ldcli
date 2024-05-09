package cmd

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

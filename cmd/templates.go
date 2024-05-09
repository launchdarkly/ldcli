package cmd

func getUsageTemplate() string {
	return `Usage:
  {{.CommandPath}} [command]

Commands:
  {{rpad "setup" 29}} Setup guide to create your first feature flag
  {{rpad "config" 29}} View and modify specific configuration values
  {{rpad "completion" 29}} Generate the autocompletion script for the specified shell

Resource Commands:
  {{rpad "flags" 29}} List, create, modify, and delete feature flags, their statuses, and their expiring targets
  {{rpad "environments" 29}} Create, delete, and update environments
  {{rpad "projects" 29}} Create, destroy, and manage projects
  {{rpad "members" 29}} Invite new members to an account
  {{rpad "segments" 29}} List, create, modify, and delete segments
  {{rpad "..." 29}} To see more resource commands, run 'ldcli resources help'

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
`
}

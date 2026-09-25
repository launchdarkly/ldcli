// Command dump for the parity harness.
//
// This is a separate program from ldcli. It prints every command the Go CLI
// registers, including hidden commands, so the coverage gate does not depend
// on visible help text. It does not change ldcli's behavior.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd"
	resourcecmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/dev_server"
	"github.com/launchdarkly/ldcli/internal/environments"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/members"
	"github.com/launchdarkly/ldcli/internal/projects"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func main() {
	version := "test"
	if len(os.Args) > 1 && os.Args[1] != "" {
		version = os.Args[1]
	}

	clients := cmd.APIClients{
		DevClient:          dev_server.NewClient(version),
		EnvironmentsClient: environments.NewClient(version),
		FlagsClient:        flags.NewClient(version),
		MembersClient:      members.NewClient(version),
		ProjectsClient:     projects.NewClient(version),
		ResourcesClient:    resources.NewClient(version),
	}
	// useConfigFile is false so dumping the tree does not create config.yml.
	root, err := cmd.NewRootCommand(
		config.NewService(resources.NewClient(version)),
		analytics.ClientFn{Version: version}.Tracker,
		clients,
		version,
		false,
		func() bool { return false },
		nil,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	c := root.Cmd()
	// Match cmd.Execute: completion is installed there, not inside NewRootCommand.
	c.InitDefaultCompletionCmd()
	completionCmd, _, findErr := c.Find([]string{"completion"})
	if findErr == nil {
		completionCmd.Long = fmt.Sprintf(`Generate the autocompletion script for %[1]s for the specified shell.
See each command's help for details on how to use the generated script.`, c.Name())
		completionCmd.SetUsageTemplate(resourcecmd.SubcommandUsageTemplate())
		c.AddCommand(completionCmd)
	}
	c.InitDefaultHelpCmd()

	var lines []string
	seen := map[string]bool{}
	walk(c, nil, &lines, seen)
	fmt.Print(strings.Join(lines, "\n"))
	if len(lines) > 0 {
		fmt.Print("\n")
	}
}

func walk(c *cobra.Command, ancestors []string, out *[]string, seen map[string]bool) {
	var path []string
	if c.Name() != "" {
		path = append(append([]string{}, ancestors...), c.Name())
	}
	line := strings.Join(path, " ")
	// Execute re-adds the completion command, and the generator can register
	// the same path twice. Coverage is per user-facing path.
	if !seen[line] {
		seen[line] = true
		*out = append(*out, line)
	}
	for _, child := range c.Commands() {
		walk(child, path, out, seen)
	}
}

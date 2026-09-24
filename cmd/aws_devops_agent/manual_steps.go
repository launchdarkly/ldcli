package awsdevopsagent

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

func manualStepPrompt(step awsdevops.ManualStep) string {
	switch step.Kind {
	case awsdevops.ManualStepOAuthConsent:
		return "Approve the LaunchDarkly MCP server consent screen at:"
	case awsdevops.ManualStepMCPServer:
		return fmt.Sprintf("Add the LaunchDarkly MCP server (%s) under Settings at:", awsdevops.MCPServerEndpoint)
	case awsdevops.ManualStepGitHubApp:
		return "Register GitHub and install the GitHub App at:"
	case awsdevops.ManualStepKiroAPIKey:
		return "Create a Kiro API key (optional) at:"
	default:
		return step.Description
	}
}

// walkManualSteps pauses on each browser-only step so the operator can finish
// it, then picks up whatever they created. Steps they skip are returned so the
// caller can still list them.
func walkManualSteps(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts awsdevops.SetupOptions,
	result *awsdevops.SetupResult,
) []awsdevops.ManualStep {
	out := cmd.OutOrStdout()
	stdin := bufio.NewReader(cmd.InOrStdin())

	var skipped []awsdevops.ManualStep
	for _, step := range result.RemainingManualSteps {
		_, _ = fmt.Fprintf(out, "\n%s\n  %s\n\n", manualStepPrompt(step), step.URL)

		if step.Kind == awsdevops.ManualStepGitHubApp {
			if serviceID := awaitGitHubService(cmd, clients, stdin); serviceID != "" {
				associateGitHub(cmd, clients, opts, result, serviceID)

				continue
			}
			skipped = append(skipped, step)

			continue
		}

		_, _ = fmt.Fprint(out, "Press Enter when you're done, or s to skip: ")
		if readLine(stdin) == "s" {
			skipped = append(skipped, step)
		}
	}

	return skipped
}

func awaitGitHubService(cmd *cobra.Command, clients awsdevops.Clients, stdin *bufio.Reader) string {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprint(out, "Waiting for the GitHub registration, or press Enter to skip... ")

	found := make(chan string, 1)
	failed := make(chan error, 1)
	go func() {
		serviceID, err := awsdevops.WaitForGitHubService(cmd.Context(), clients, 0)
		if err != nil {
			failed <- err

			return
		}
		found <- serviceID
	}()

	entered := make(chan struct{}, 1)
	go func() {
		readLine(stdin)
		entered <- struct{}{}
	}()

	select {
	case serviceID := <-found:
		_, _ = fmt.Fprintf(out, "found service %s\n", serviceID)

		return serviceID
	case err := <-failed:
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "\nstopped watching for the GitHub registration: %s\n", err)
	case <-entered:
		_, _ = fmt.Fprintln(out, "skipped")
	}

	return ""
}

func associateGitHub(
	cmd *cobra.Command,
	clients awsdevops.Clients,
	opts awsdevops.SetupOptions,
	result *awsdevops.SetupResult,
	serviceID string,
) {
	out := cmd.OutOrStdout()
	if opts.GitHubOwner == "" || opts.GitHubRepo == "" || opts.GitHubRepoID == "" {
		_, _ = fmt.Fprintln(
			out,
			"GitHub is registered. Re-run setup with --github-owner <owner> --github-repo <repo> --github-repo-id <id> to connect a repository",
		)

		return
	}

	opts.GitHubServiceID = serviceID
	associationID, err := awsdevops.AssociateGitHub(cmd.Context(), clients, result.AgentSpaceID, opts)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", err)

		return
	}
	result.GitHubAssociationID = associationID
	_, _ = fmt.Fprintf(out, "Connected %s/%s\n", opts.GitHubOwner, opts.GitHubRepo)
}

func readLine(stdin *bufio.Reader) string {
	line, err := stdin.ReadString('\n')
	if err != nil && err != io.EOF {
		return ""
	}

	return trimLine(line)
}

func trimLine(line string) string {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r' || line[len(line)-1] == ' ') {
		line = line[:len(line)-1]
	}

	return line
}

func canPrompt() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

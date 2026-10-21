package awsdevopsagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// ssoLogin offers aws sso login on expired credentials.
type ssoLogin struct {
	newClients func(ctx context.Context, region, profile string) (awsdevops.Clients, error)
	configPath string
	in         io.Reader
	out        io.Writer
	tty        func() bool
	yes        bool
	lookPath   func(string) (string, error)
	run        func(ctx context.Context, name string, args ...string) error
}

// loadClients builds AWS clients from the command flags.
func loadClients(cmd *cobra.Command) (awsdevops.Clients, error) {
	l := newSSOLogin(cmd, canPrompt)

	return l.clients(cmd.Context(), mustString(cmd, regionFlag), mustString(cmd, profileFlag))
}

// newSSOLogin logs in only in a terminal, because aws sso login needs a
// person to finish the sign-in in a browser. It asks first unless --yes is
// set. The prompt goes to stderr, so a json run can ask too.
func newSSOLogin(cmd *cobra.Command, tty func() bool) ssoLogin {
	return ssoLogin{
		newClients: awsdevops.NewClients,
		configPath: awsdevops.ConfigFilePath(),
		in:         sharedStdin(cmd),
		out:        cmd.ErrOrStderr(),
		tty:        tty,
		yes:        yesSet(cmd),
		lookPath:   exec.LookPath,
		run:        runAttached,
	}
}

func (l ssoLogin) clients(ctx context.Context, region, profile string) (awsdevops.Clients, error) {
	clients, err := l.newClients(ctx, region, profile)
	if !errors.Is(err, awsdevops.ErrNoCredentials) {
		return clients, err
	}

	// A login helps only if the SSO token is expired or missing.
	name := awsdevops.SSOProfile(l.configPath, profile)
	if name == "" || !awsdevops.SSOTokenInvalid(err) || !l.tty() {
		return clients, err
	}
	if _, lookErr := l.lookPath("aws"); lookErr != nil {
		return clients, err
	}

	if l.yes {
		_, _ = fmt.Fprintf(l.out, "Running aws sso login --profile %s\n", name)
	} else {
		_, _ = fmt.Fprintf(l.out, "Run aws sso login --profile %s now? [y/N] ", name)
		answer := strings.ToLower(readLine(l.in))
		if answer != "y" && answer != "yes" {
			return clients, err
		}
	}

	if runErr := l.run(ctx, "aws", "sso", "login", "--profile", name); runErr != nil {
		return awsdevops.Clients{}, fmt.Errorf("aws sso login --profile %s failed: %w", name, runErr)
	}

	clients, err = l.newClients(ctx, region, profile)
	if awsdevops.SSOTokenInvalid(err) {
		return clients, fmt.Errorf(
			"the SSO token for profile %s is not valid after aws sso login: %w", name, errors.Unwrap(err))
	}

	return clients, err
}

// yesSet reports a passed --yes flag.
func yesSet(cmd *cobra.Command) bool {
	yes, err := cmd.Flags().GetBool(yesFlag)

	return err == nil && yes
}

// runAttached runs a command on the terminal.
func runAttached(ctx context.Context, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stderr // keep stdout for results
	c.Stderr = os.Stderr

	return c.Run()
}

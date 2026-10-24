package awsdevopsagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// ssoLogin offers aws sso login on expired credentials. In a terminal, it
// also asks for a profile and a region that are missing.
type ssoLogin struct {
	newClients func(ctx context.Context, region, profile string) (awsdevops.Clients, error)
	configPath string
	in         io.Reader
	out        io.Writer
	tty        func() bool
	yes        bool
	lookPath   func(string) (string, error)
	run        func(ctx context.Context, name string, args ...string) error
	choose     chooser
	tokenPath  func(key string) (string, error)
	now        func() time.Time
}

// loadClients builds AWS clients from the command flags. It sets --profile
// to a profile that the user picks, so the output names it.
func loadClients(cmd *cobra.Command) (awsdevops.Clients, error) {
	l := newSSOLogin(cmd, canPrompt)
	setProfile := func(name string) { _ = cmd.Flags().Set(profileFlag, name) }

	return l.load(cmd.Context(), mustString(cmd, regionFlag), mustString(cmd, profileFlag), setProfile)
}

// load builds clients. If no profile is selected and the default
// credentials do not work, it asks for a profile. setProfile gets the
// profile that the user picks.
func (l ssoLogin) load(ctx context.Context, region, profile string, setProfile func(string)) (awsdevops.Clients, error) {
	if !awsdevops.DefaultFallback(profile) {
		return l.clients(ctx, region, profile)
	}

	clients, err := l.newClients(ctx, region, profile)
	// NewClients looks for the region before the credentials. If there is no
	// default profile, a missing region also means that no profile is
	// selected, and the profile can give the region.
	noProfile := errors.Is(err, awsdevops.ErrNoCredentials) ||
		errors.Is(err, awsdevops.ErrNoRegion) && !awsdevops.HasProfile("default")
	if !noProfile {
		return l.clientsAfter(ctx, region, profile, clients, err)
	}

	name, pickErr := l.pickProfile()
	if pickErr != nil {
		return awsdevops.Clients{}, pickErr
	}
	if name == "" {
		return l.clientsAfter(ctx, region, profile, clients, err)
	}
	setProfile(name)

	return l.clients(ctx, region, name)
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
		choose:     teaChooser(sharedStdin(cmd), cmd.ErrOrStderr()),
		tokenPath:  ssocreds.StandardCachedTokenFilepath,
		now:        time.Now,
	}
}

func (l ssoLogin) clients(ctx context.Context, region, profile string) (awsdevops.Clients, error) {
	clients, err := l.newClients(ctx, region, profile)

	return l.clientsAfter(ctx, region, profile, clients, err)
}

// clientsAfter handles the result of a newClients call. In a terminal, it
// asks for a missing region, and it offers aws sso login on an expired SSO
// token.
func (l ssoLogin) clientsAfter(
	ctx context.Context,
	region, profile string,
	clients awsdevops.Clients,
	err error,
) (awsdevops.Clients, error) {
	if errors.Is(err, awsdevops.ErrNoRegion) && l.tty() && !l.yes {
		picked, pickErr := l.pickRegion()
		if pickErr != nil {
			return awsdevops.Clients{}, pickErr
		}
		if picked == "" {
			return clients, err
		}
		region = picked
		clients, err = l.newClients(ctx, region, profile)
	}
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
		return clients, &stillInvalidErr{profile: name, err: err}
	}

	return clients, err
}

// stillInvalidErr reports an SSO token that is not valid after a login. Its
// message does not include err. The text of err tells the user to run aws sso
// login, which the user just did, and the SDK cause can contain secrets.
type stillInvalidErr struct {
	profile string
	err     error
}

func (e *stillInvalidErr) Error() string {
	return fmt.Sprintf("the SSO token for profile %s is not valid after aws sso login", e.profile)
}

func (e *stillInvalidErr) Unwrap() error { return e.err }

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

package awsdevopsagent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// pickerRows is the number of rows that a picker shows. A longer list scrolls
// in place, so the picker never takes more lines than this.
const pickerRows = 10

// errPickCanceled is the result of a picker that the user closed.
var errPickCanceled = errors.New("canceled")

// chooser asks the user to choose one label. The highlight starts on the
// label at index start. It returns the index of the chosen label, or
// errPickCanceled.
type chooser func(title, description string, labels []string, start int) (int, error)

// newChooser gives the chooser of a command. Tests replace it, because the
// picker needs a terminal.
var newChooser = func(cmd *cobra.Command) chooser {
	return teaChooser(sharedStdin(cmd), cmd.ErrOrStderr())
}

// pickProfile asks which profile to use. The signed-in SSO profiles come
// first, so Enter picks one of them. With --yes in a terminal, it uses the
// one signed-in profile. It returns an empty name if it cannot ask, so the
// caller reports the usual error.
func (l ssoLogin) pickProfile() (string, error) {
	if !l.tty() {
		return "", nil
	}
	profiles := awsdevops.Profiles(l.tokenPath, l.now())
	if len(profiles) == 0 {
		return "", nil
	}

	if l.yes {
		if signedIn(profiles) != 1 {
			return "", nil
		}
		_, _ = fmt.Fprintf(l.out, "Using AWS profile %s, which has an SSO session\n", profiles[0].Name)

		return profiles[0].Name, nil
	}

	choice, err := l.choose(
		"Which AWS profile should ldcli use?",
		// The picker cuts a line wider than the terminal, so this fits 80 columns.
		"The default credentials did not work. ✓ means signed in. Type to search.",
		profileLabels(profiles),
		0,
	)
	if errors.Is(err, errPickCanceled) {
		return "", fmt.Errorf("no AWS profile was chosen. Pass --%s <name> or run `export AWS_PROFILE=<name>`", profileFlag)
	}
	if err != nil {
		return "", err
	}

	name := profiles[choice].Name
	_, _ = fmt.Fprintf(l.out, "Using AWS profile %s. To skip this question, pass --%s %s or run `export AWS_PROFILE=%s`\n",
		name, profileFlag, name, name)

	return name, nil
}

// pickRegion asks which supported region to use. It returns an empty region
// if the user closes the picker.
func (l ssoLogin) pickRegion() (string, error) {
	choice, err := l.choose(
		"Which AWS region should ldcli use?",
		"No AWS region is set. Type to search.",
		awsdevops.SupportedRegions,
		0,
	)
	if errors.Is(err, errPickCanceled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	region := awsdevops.SupportedRegions[choice]
	_, _ = fmt.Fprintf(l.out, "Using region %s. To skip this question, pass --%s %s or run `export AWS_REGION=%s`\n",
		region, regionFlag, region, region)

	return region, nil
}

// signedIn counts the signed-in profiles.
func signedIn(profiles []awsdevops.Profile) int {
	count := 0
	for _, p := range profiles {
		if p.SignedIn {
			count++
		}
	}

	return count
}

// profileLabels gives each profile one line, in columns: a check mark for a
// profile with an SSO session, the name, account, region, and role. The role
// is last because it is the longest, and the picker cuts a line that is
// wider than the terminal. The search matches any part of the line, for
// example an account ID.
func profileLabels(profiles []awsdevops.Profile) []string {
	var nameWidth, accountWidth, regionWidth int
	for _, p := range profiles {
		nameWidth = max(nameWidth, ansi.StringWidth(p.Name))
		accountWidth = max(accountWidth, ansi.StringWidth(p.Account))
		regionWidth = max(regionWidth, ansi.StringWidth(p.Region))
	}

	labels := make([]string, len(profiles))
	for i, p := range profiles {
		mark := " "
		if p.SignedIn {
			mark = "✓"
		}
		line := mark + " " + padRight(p.Name, nameWidth) + "  " + padRight(p.Account, accountWidth) +
			"  " + padRight(p.Region, regionWidth) + "  " + p.Role
		labels[i] = strings.TrimRight(line, " ")
	}

	return labels
}

// padRight adds spaces after text up to the width in terminal columns. A
// wide character, for example in a profile name, takes two columns.
func padRight(text string, width int) string {
	return text + strings.Repeat(" ", max(width-ansi.StringWidth(text), 0))
}

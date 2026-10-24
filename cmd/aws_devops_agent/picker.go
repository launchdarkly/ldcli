package awsdevopsagent

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

// pickerRows is the number of options that a picker shows. A longer list
// scrolls in place, so the picker never takes more lines than this.
const pickerRows = 10

// errPickCanceled is the result of a picker that the user closed.
var errPickCanceled = errors.New("canceled")

// chooser asks the user to choose one label. It returns the index of the
// label, or errPickCanceled.
type chooser func(title, description string, labels []string) (int, error)

// huhChooser asks below the current output, so the text above the picker
// stays on the screen. Press / to search the labels.
func huhChooser(in io.Reader, out io.Writer) chooser {
	return func(title, description string, labels []string) (int, error) {
		selected := 0
		err := huh.NewForm(huh.NewGroup(pickerField(title, description, labels, &selected))).
			WithInput(in).
			WithOutput(out).
			WithAccessible(false).
			Run()
		if errors.Is(err, huh.ErrUserAborted) {
			return 0, errPickCanceled
		}

		return selected, err
	}
}

// pickerField is a select field that shows pickerRows labels at most.
func pickerField(title, description string, labels []string, selected *int) *huh.Select[int] {
	options := make([]huh.Option[int], len(labels))
	for i, label := range labels {
		options[i] = huh.NewOption(label, i)
	}
	field := huh.NewSelect[int]().
		Title(title).
		Description(description).
		Options(options...).
		Value(selected)
	if len(labels) > pickerRows {
		// The height includes the title and the description.
		field.Height(pickerRows + 2)
	}

	return field
}

// pickProfile asks which profile to use. The signed-in SSO profiles come
// first, so Enter picks one of them. With --yes, it uses the one signed-in
// profile. It returns an empty name if it cannot ask, so the caller reports
// the usual error.
func (l ssoLogin) pickProfile() (string, error) {
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
	if !l.tty() {
		return "", nil
	}

	choice, err := l.choose(
		"Which AWS profile should ldcli use?",
		// The picker cuts a line wider than the terminal, so this fits 80 columns.
		"The default credentials did not work. ✓ means signed in. Press / to search.",
		profileLabels(profiles),
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
		"No AWS region is set. Press / to search.",
		awsdevops.SupportedRegions,
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
		nameWidth = max(nameWidth, len(p.Name))
		accountWidth = max(accountWidth, len(p.Account))
		regionWidth = max(regionWidth, len(p.Region))
	}

	labels := make([]string, len(profiles))
	for i, p := range profiles {
		mark := " "
		if p.SignedIn {
			mark = "✓"
		}
		line := fmt.Sprintf("%s %-*s  %-*s  %-*s  %s",
			mark, nameWidth, p.Name, accountWidth, p.Account, regionWidth, p.Region, p.Role)
		labels[i] = strings.TrimRight(line, " ")
	}

	return labels
}

package awsdevops

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// MinAWSCLIVersion is the first AWS CLI release with `aws devops-agent`.
var MinAWSCLIVersion = [3]int{2, 36, 0}

var awsCLIVersionPattern = regexp.MustCompile(`aws-cli/(\d+)\.(\d+)\.(\d+)`)

// CheckAWSCLIVersion reports why the installed AWS CLI cannot run
// `aws devops-agent` commands, and returns an empty string when it can. The
// CLI itself is not required by this package, so callers should warn rather
// than fail.
func CheckAWSCLIVersion(ctx context.Context) string {
	path, err := exec.LookPath("aws")
	if err != nil {
		return fmt.Sprintf(
			"The AWS CLI is not installed. This command does not need it, but `aws devops-agent` needs %s or later.",
			formatVersion(MinAWSCLIVersion),
		)
	}

	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Unable to determine the AWS CLI version: %s", err)
	}

	version, ok := parseAWSCLIVersion(string(out))
	if !ok {
		return fmt.Sprintf("Unable to determine the AWS CLI version from %q", strings.TrimSpace(string(out)))
	}
	if slices.Compare(version[:], MinAWSCLIVersion[:]) < 0 {
		return fmt.Sprintf(
			"AWS CLI %s does not support `aws devops-agent`. This command does not need it, but upgrade to %s or later to run those commands yourself (https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html).",
			formatVersion(version),
			formatVersion(MinAWSCLIVersion),
		)
	}

	return ""
}

func parseAWSCLIVersion(output string) ([3]int, bool) {
	match := awsCLIVersionPattern.FindStringSubmatch(output)
	if match == nil {
		return [3]int{}, false
	}

	var version [3]int
	for i := range version {
		part, err := strconv.Atoi(match[i+1])
		if err != nil {
			return [3]int{}, false
		}
		version[i] = part
	}

	return version, true
}

func formatVersion(version [3]int) string {
	return fmt.Sprintf("%d.%d.%d", version[0], version[1], version[2])
}

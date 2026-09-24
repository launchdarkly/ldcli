package awsdevops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// KiroSecretName is the GitHub Actions secret the agent's workflow reads the
// Kiro API key from.
const KiroSecretName = "KIRO_API_KEY"

// KiroSecretCommand is the command that stores a Kiro API key on a repository.
func KiroSecretCommand(owner, repo string) string {
	target := "<owner>/<repo>"
	if owner != "" && repo != "" {
		target = owner + "/" + repo
	}

	return fmt.Sprintf("gh secret set %s --repo %s", KiroSecretName, target)
}

// StoreKiroAPIKey saves the key as a GitHub Actions secret on the repository
// the agent reviews, using the GitHub CLI's credentials.
func StoreKiroAPIKey(ctx context.Context, owner, repo, key string) error {
	if owner == "" || repo == "" {
		return fmt.Errorf(
			"no repository to store the key on: pass --github-owner and --github-repo, or run '%s' yourself",
			KiroSecretCommand(owner, repo),
		)
	}

	path, err := exec.LookPath("gh")
	if err != nil {
		return fmt.Errorf("the GitHub CLI is not installed, so run '%s' once you have it", KiroSecretCommand(owner, repo))
	}

	cmd := exec.CommandContext(ctx, path, "secret", "set", KiroSecretName, "--repo", owner+"/"+repo, "--body", key)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}

		return errors.New("unable to store " + KiroSecretName + ": " + message)
	}

	return nil
}

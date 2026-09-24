package awsdevops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GitHubRepo identifies a repository the way AssociateService expects it.
type GitHubRepo struct {
	ID        string
	OwnerType string
}

// LookupGitHubRepo reads the repository's numeric ID and owner type through
// the GitHub CLI, so callers do not have to look them up by hand.
func LookupGitHubRepo(ctx context.Context, owner, repo string) (GitHubRepo, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return GitHubRepo{}, fmt.Errorf("the GitHub CLI is not installed, so pass --github-repo-id for %s/%s", owner, repo)
	}

	out, err := exec.CommandContext(ctx, path, "api", "repos/"+owner+"/"+repo).Output()
	if err != nil {
		return GitHubRepo{}, fmt.Errorf(
			"unable to read %s/%s from GitHub: %s. Check the name and that 'gh auth status' can see it, or pass --github-repo-id",
			owner,
			repo,
			ghError(err),
		)
	}

	var body struct {
		ID    int64 `json:"id"`
		Owner struct {
			Type string `json:"type"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return GitHubRepo{}, fmt.Errorf("unable to read %s/%s from GitHub, so pass --github-repo-id: %w", owner, repo, err)
	}

	return GitHubRepo{
		ID:        strconv.FormatInt(body.ID, 10),
		OwnerType: strings.ToLower(body.Owner.Type),
	}, nil
}

// ghError prefers what the GitHub CLI printed to stderr over its exit status.
func ghError(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if message := strings.TrimSpace(string(exitErr.Stderr)); message != "" {
			return strings.SplitN(message, "\n", 2)[0]
		}
	}

	return err.Error()
}

package awsdevopsagent

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/launchdarkly/ldcli/internal/awsdevops"
)

const (
	githubRepoFlag = "github-repo"
	repoDirFlag    = "repo-dir"

	kiroSecretName       = "KIRO_API_KEY"
	defaultBranchMarker  = "__DEFAULT_BRANCH__"
	kiroAPIKeyPortalURL  = "https://app.kiro.dev"
	githubConsoleSteps   = "Capabilities > Pipeline > Add > GitHub. Pick Read & Write, and install the app only on "
	kiroFileDescriptions = "the Kiro agent and the implement and merge workflows"
)

//go:embed kirofiles/*
var kiroFiles embed.FS

// kiroFile maps an embedded template to its path in the repository.
type kiroFile struct {
	template string
	path     string
}

var kiroFileList = []kiroFile{
	{"kirofiles/flag-implementer.json", ".kiro/agents/flag-implementer.json"},
	{"kirofiles/kiro-implement.yml", ".github/workflows/kiro-implement.yml"},
	{"kirofiles/merge-pr.yml", ".github/workflows/merge-pr.yml"},
}

// addKiroFlags adds the flags for the GitHub and Kiro step of setup.
func addKiroFlags(cmd *cobra.Command) {
	cmd.Flags().String(githubRepoFlag, "", "GitHub repository (owner/name) the agent works in. If set, setup also connects GitHub and prepares the repository for Kiro. Needs the gh CLI")
	cmd.Flags().String(repoDirFlag, ".", "Local checkout of --"+githubRepoFlag+", where setup writes the Kiro files")
}

// setupGitHubAndKiro connects the repository to the agent space and prepares
// it for headless Kiro runs. It is a prototype: every step after the
// association uses the gh CLI as the person who runs setup.
func setupGitHubAndKiro(cmd *cobra.Command, clients awsdevops.Clients, agentSpaceID string, ask bool) error {
	fullName := mustString(cmd, githubRepoFlag)
	if fullName == "" {
		return nil
	}
	out := cmd.ErrOrStderr()
	logf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, format+"\n", args...)
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("--%s needs the GitHub CLI. Install gh and run gh auth login, then run setup again", githubRepoFlag)
	}

	_, _ = fmt.Fprintf(out, "\nGitHub and Kiro for %s\n", fullName)
	repo, defaultBranch, err := lookUpRepo(fullName)
	if err != nil {
		return err
	}

	// 1. The account-level GitHub registration. Only the console can make it.
	serviceID, err := awsdevops.GitHubRegistration(cmd.Context(), clients, repo.Owner)
	if err != nil {
		return err
	}
	if serviceID == "" {
		console := awsdevops.ConsoleURL(clients.Region)
		logf("This AWS account has no GitHub registration for %s.", repo.Owner)
		logf("In the AWS console, open %s%s.", githubConsoleSteps, repo.FullName())
		logf("  %s", console)
		_ = openURL(console)
		serviceID, err = awsdevops.WaitForGitHubRegistration(cmd.Context(), clients, repo.Owner, logf)
		if err != nil {
			return err
		}
	}
	logf("Found the GitHub registration for %s (%s)", repo.Owner, serviceID)

	// 2. The repository in the agent space.
	associationID, existed, err := awsdevops.ConnectGitHubRepo(cmd.Context(), clients, agentSpaceID, serviceID, repo)
	if err != nil {
		return err
	}
	if existed {
		logf("%s is already connected to the agent space (%s)", repo.FullName(), associationID)
	} else {
		logf("Connected %s to the agent space (%s)", repo.FullName(), associationID)
	}

	// 3. The Kiro API key as a repository secret.
	if err := storeKiroKey(cmd, repo, ask, logf); err != nil {
		return err
	}

	// 4. Let the GITHUB_TOKEN of a workflow open pull requests.
	if err := allowActionsPullRequests(repo, logf); err != nil {
		return err
	}

	// 5. The Kiro agent and the two workflows.
	dir := mustString(cmd, repoDirFlag)
	written, err := writeKiroFiles(dir, defaultBranch, logf)
	if err != nil {
		return err
	}

	// 6. The agent dispatches the deploy workflow, so it needs workflow_dispatch.
	warnNoDispatchableDeploy(dir, logf)

	// 7. Branch protection keeps the agent configuration under review.
	if err := offerBranchProtection(cmd, repo, defaultBranch, ask, logf); err != nil {
		return err
	}

	if len(written) > 0 {
		_, _ = fmt.Fprintf(out, "\nSetup wrote %s. Review and commit them:\n", kiroFileDescriptions)
		_, _ = fmt.Fprintf(out, "  git add %s\n", strings.Join(written, " "))
		_, _ = fmt.Fprintln(out, "  git commit -m \"Add the Kiro agent and workflows for AWS DevOps Agent\"")
	}

	return nil
}

var openURL = browser.OpenURL

// lookUpRepo reads the repository ID, owner type and default branch.
func lookUpRepo(fullName string) (awsdevops.GitHubRepo, string, error) {
	owner, name, ok := strings.Cut(fullName, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return awsdevops.GitHubRepo{}, "", fmt.Errorf("--%s must be owner/name, for example acme/shop", githubRepoFlag)
	}
	raw, err := gh(nil, "api", "repos/"+fullName)
	if err != nil {
		return awsdevops.GitHubRepo{}, "", fmt.Errorf("unable to read %s with gh: %w", fullName, err)
	}
	var data struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"owner"`
		Permissions struct {
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return awsdevops.GitHubRepo{}, "", fmt.Errorf("unable to read the gh output for %s: %w", fullName, err)
	}
	if !data.Permissions.Admin {
		return awsdevops.GitHubRepo{}, "", fmt.Errorf("you need admin access to %s to store a secret and change its Actions settings", fullName)
	}

	return awsdevops.GitHubRepo{
		Owner:        data.Owner.Login,
		Name:         data.Name,
		ID:           strconv.FormatInt(data.ID, 10),
		Organization: data.Owner.Type == "Organization",
	}, data.DefaultBranch, nil
}

// storeKiroKey asks for the Kiro API key and stores it as a repository secret.
// If the secret exists, setup keeps it.
func storeKiroKey(cmd *cobra.Command, repo awsdevops.GitHubRepo, ask bool, logf func(string, ...any)) error {
	raw, err := gh(nil, "secret", "list", "--repo", repo.FullName(), "--json", "name")
	if err != nil {
		return fmt.Errorf("unable to list the secrets of %s: %w", repo.FullName(), err)
	}
	var secrets []struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &secrets)
	for _, secret := range secrets {
		if secret.Name == kiroSecretName {
			logf("%s already has the %s secret", repo.FullName(), kiroSecretName)
			return nil
		}
	}

	key := strings.TrimSpace(os.Getenv(kiroSecretName))
	if key == "" {
		if !ask {
			return fmt.Errorf("set %s in the environment, or run setup in a terminal to type it", kiroSecretName)
		}
		logf("Kiro needs an API key to run with no person present. Make one in your account settings at %s.", kiroAPIKeyPortalURL)
		logf("The key needs a Kiro Pro, Pro+, Pro Max or Power plan.")
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), "Kiro API key (input is hidden): ")
		bytesKey, err := term.ReadPassword(int(os.Stdin.Fd()))
		_, _ = fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return fmt.Errorf("unable to read the Kiro API key: %w", err)
		}
		key = strings.TrimSpace(string(bytesKey))
	}
	if key == "" {
		return errors.New("no Kiro API key was given")
	}

	// gh reads the value from stdin, so the key is not in the process list.
	if _, err := gh(strings.NewReader(key), "secret", "set", kiroSecretName, "--repo", repo.FullName()); err != nil {
		return fmt.Errorf("unable to store the %s secret: %w", kiroSecretName, err)
	}
	logf("Stored the %s secret in %s", kiroSecretName, repo.FullName())

	return nil
}

// allowActionsPullRequests turns on "Allow GitHub Actions to create and
// approve pull requests". It keeps the default token permissions as they are.
func allowActionsPullRequests(repo awsdevops.GitHubRepo, logf func(string, ...any)) error {
	path := "repos/" + repo.FullName() + "/actions/permissions/workflow"
	raw, err := gh(nil, "api", path)
	if err != nil {
		return fmt.Errorf("unable to read the Actions settings of %s: %w", repo.FullName(), err)
	}
	var current struct {
		DefaultWorkflowPermissions   string `json:"default_workflow_permissions"`
		CanApprovePullRequestReviews bool   `json:"can_approve_pull_request_reviews"`
	}
	_ = json.Unmarshal(raw, &current)
	if current.CanApprovePullRequestReviews {
		logf("GitHub Actions can already open pull requests in %s", repo.FullName())
		return nil
	}

	_, err = gh(nil, "api", "-X", "PUT", path,
		"-f", "default_workflow_permissions="+current.DefaultWorkflowPermissions,
		"-F", "can_approve_pull_request_reviews=true")
	if err != nil {
		return fmt.Errorf(
			"unable to let GitHub Actions open pull requests in %s. If an organization policy blocks it, "+
				"turn it on in Organization Settings > Actions > General: %w",
			repo.FullName(), err)
	}
	logf("Let GitHub Actions open pull requests in %s", repo.FullName())

	return nil
}

// writeKiroFiles writes each Kiro file that the checkout does not have yet.
// It returns the paths it wrote.
func writeKiroFiles(dir, defaultBranch string, logf func(string, ...any)) ([]string, error) {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return nil, fmt.Errorf("%s is not the root of a git checkout. Pass --%s with the checkout of the repository", dir, repoDirFlag)
	}
	var written []string
	for _, file := range kiroFileList {
		target := filepath.Join(dir, file.path)
		if _, err := os.Stat(target); err == nil {
			logf("Kept %s, which already exists", file.path)
			continue
		}
		body, err := kiroFiles.ReadFile(file.template)
		if err != nil {
			return written, err
		}
		body = bytes.ReplaceAll(body, []byte(defaultBranchMarker), []byte(defaultBranch))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			return written, err
		}
		logf("Wrote %s", file.path)
		written = append(written, file.path)
	}

	return written, nil
}

// warnNoDispatchableDeploy warns when no other workflow has a
// workflow_dispatch trigger. Setup cannot know which workflow deploys, so it
// does not change one.
func warnNoDispatchableDeploy(dir string, logf func(string, ...any)) {
	own := map[string]bool{}
	for _, file := range kiroFileList {
		own[filepath.Base(file.path)] = true
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".github", "workflows", "*.y*ml"))
	for _, match := range matches {
		if own[filepath.Base(match)] {
			continue
		}
		body, err := os.ReadFile(match)
		if err == nil && bytes.Contains(body, []byte("workflow_dispatch")) {
			return
		}
	}
	logf("Warning: no deploy workflow has a workflow_dispatch trigger. " +
		"The agent starts deployment with it. Add \"workflow_dispatch:\" under \"on:\" in your deploy workflow")
}

// offerBranchProtection asks before it protects the default branch, because
// the rule changes how everyone merges to it.
func offerBranchProtection(cmd *cobra.Command, repo awsdevops.GitHubRepo, branch string, ask bool, logf func(string, ...any)) error {
	path := fmt.Sprintf("repos/%s/branches/%s/protection", repo.FullName(), branch)
	if _, err := gh(nil, "api", path); err == nil {
		logf("%s already has branch protection on %s", repo.FullName(), branch)
		return nil
	}
	logf("Anyone who can push to %s can change what the agent is allowed to do.", branch)
	if !ask {
		logf("Warning: %s has no branch protection on %s. Require pull requests and passing status checks", repo.FullName(), branch)
		return nil
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Require pull requests and passing status checks on %s? [Y/n]: ", branch)
	if !readYes(sharedStdin(cmd)) {
		logf("Skipped branch protection")
		return nil
	}

	rule := `{"required_status_checks":{"strict":true,"contexts":[]},` +
		`"enforce_admins":false,` +
		`"required_pull_request_reviews":{"required_approving_review_count":1},` +
		`"restrictions":null}`
	if _, err := gh(strings.NewReader(rule), "api", "-X", "PUT", path, "--input", "-"); err != nil {
		return fmt.Errorf("unable to protect %s in %s: %w", branch, repo.FullName(), err)
	}
	logf("Protected %s in %s", branch, repo.FullName())

	return nil
}

// gh runs the GitHub CLI and returns its stdout.
func gh(stdin *strings.Reader, args ...string) ([]byte, error) {
	command := exec.Command("gh", args...)
	if stdin != nil {
		command.Stdin = stdin
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return out, fmt.Errorf("gh %s: %s", strings.Join(args[:min(2, len(args))], " "), strings.TrimSpace(stderr.String()))
	}

	return out, nil
}

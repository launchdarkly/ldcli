package awsdevops

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

//go:embed repofiles/flag-implementer.json
var flagImplementerAgent []byte

//go:embed repofiles/kiro-implement.yml
var kiroImplementWorkflow []byte

//go:embed repofiles/merge-pr.yml
var mergePRWorkflow []byte

//go:embed repofiles/experiment-orchestration.md
var experimentOrchestrationSkill []byte

// DefaultSkillName is the skill name the CLI attaches to when it creates the
// experiment-orchestration skill from the embedded body.
const DefaultSkillName = "experiment-orchestration"

// AgentFile is a repo path plus the bytes to write there.
type AgentFile struct {
	Path    string
	Content []byte
	Message string
}

// AgentFiles are the templates the AWS DevOps Agent workflow needs in a target
// repository: the Kiro persona plus the two GitHub Actions workflows.
func AgentFiles() []AgentFile {
	return []AgentFile{
		{
			Path:    ".kiro/agents/flag-implementer.json",
			Content: flagImplementerAgent,
			Message: "Add Kiro flag-implementer agent (LaunchDarkly CLI)",
		},
		{
			Path:    ".github/workflows/kiro-implement.yml",
			Content: kiroImplementWorkflow,
			Message: "Add Kiro implementation workflow (LaunchDarkly CLI)",
		},
		{
			Path:    ".github/workflows/merge-pr.yml",
			Content: mergePRWorkflow,
			Message: "Add experiment merge workflow (LaunchDarkly CLI)",
		},
	}
}

// DefaultSkillBody is the embedded experiment-orchestration skill.
func DefaultSkillBody() string {
	return string(experimentOrchestrationSkill)
}

// CommitAgentFiles creates each AgentFile that does not yet exist on the
// repository's default branch, using the GitHub CLI's credentials. Files that
// already exist are left alone so a user's edits are never overwritten.
// Workflow files that need the 'workflow' token scope are skipped with a
// hint when the current token cannot commit them, so the rest of setup can
// continue.
func CommitAgentFiles(
	ctx context.Context,
	owner, repo string,
	files []AgentFile,
	logf func(string, ...any),
) error {
	if owner == "" || repo == "" {
		return errors.New("owner and repo are required to commit repository files")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("the GitHub CLI is not installed, so the repository files cannot be committed")
	}

	warnedWorkflowScope := false
	for _, file := range files {
		created, err := createFileIfMissing(ctx, owner, repo, file)
		if err != nil {
			if isWorkflowFile(file.Path) && isMissingWorkflowScope(err) {
				if !warnedWorkflowScope {
					logf(
						"Skipping %s: the GitHub CLI's token needs the 'workflow' scope to commit files under .github/workflows/. "+
							"Re-authenticate with 'gh auth refresh --scopes workflow' and re-run setup, or commit the file yourself.",
						file.Path,
					)
					warnedWorkflowScope = true
				} else {
					logf("Skipping %s: still missing the 'workflow' token scope", file.Path)
				}

				continue
			}
			return err
		}
		if created {
			logf("Committed %s to %s/%s", file.Path, owner, repo)
		} else {
			logf("%s already exists on %s/%s, left as is", file.Path, owner, repo)
		}
	}

	return nil
}

func isWorkflowFile(path string) bool {
	return strings.HasPrefix(path, ".github/workflows/")
}

// isMissingWorkflowScope matches how GitHub reports a token without the
// 'workflow' scope trying to touch a workflow file: a 404 or 403 on the
// contents PUT for that path.
func isMissingWorkflowScope(err error) bool {
	message := err.Error()

	return strings.Contains(message, "HTTP 404") ||
		strings.Contains(message, "\"status\":\"404\"") ||
		strings.Contains(message, "HTTP 403") ||
		strings.Contains(message, "workflow`") ||
		strings.Contains(message, "workflow scope")
}

// createFileIfMissing tries to create the file at path. If GitHub reports it
// already exists, that is treated as success and reported to the caller so a
// pre-existing file is never overwritten.
func createFileIfMissing(ctx context.Context, owner, repo string, file AgentFile) (bool, error) {
	if existing, err := getFileContent(ctx, owner, repo, file.Path); err == nil {
		if bytes.Equal(existing, file.Content) {
			return false, nil
		}
		return false, nil
	} else if !isNotFound(err) {
		return false, err
	}

	body := map[string]string{
		"message": file.Message,
		"content": base64.StdEncoding.EncodeToString(file.Content),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return false, err
	}

	cmd := exec.CommandContext(ctx, "gh", "api",
		"--method", "PUT",
		"-H", "Accept: application/vnd.github+json",
		"--input", "-",
		"repos/"+owner+"/"+repo+"/contents/"+file.Path,
	)
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("unable to commit %s: %s", file.Path, ghAPIError(out, err))
	}

	return true, nil
}

// getFileContent reads a file from the repository's default branch. It returns
// a not-found error the caller can distinguish so a missing file is treated as
// "safe to create" and any other failure surfaces.
func getFileContent(ctx context.Context, owner, repo, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", "api",
		"-H", "Accept: application/vnd.github.raw+json",
		"repos/"+owner+"/"+repo+"/contents/"+path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(out, []byte("HTTP 404")) || bytes.Contains(out, []byte("Not Found")) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("unable to read %s: %s", path, ghAPIError(out, err))
	}

	return out, nil
}

var errNotFound = errors.New("not found")

func isNotFound(err error) bool { return errors.Is(err, errNotFound) }

// EnableActionsCanCreatePRs turns on "Allow GitHub Actions to create and
// approve pull requests" so the Kiro implementation workflow can open PRs.
// The default workflow permission is left as-is because the workflow files
// declare permissions explicitly.
func EnableActionsCanCreatePRs(
	ctx context.Context,
	owner, repo string,
	logf func(string, ...any),
) error {
	if owner == "" || repo == "" {
		return errors.New("owner and repo are required to change Actions permissions")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("the GitHub CLI is not installed, so the Actions permission cannot be changed")
	}

	current, err := getActionsWorkflowPermissions(ctx, owner, repo)
	if err != nil {
		return err
	}
	if current.CanApprovePRs {
		logf("Actions on %s/%s can already create pull requests, left as is", owner, repo)

		return nil
	}

	body := map[string]any{
		"default_workflow_permissions":      current.DefaultPermissions,
		"can_approve_pull_request_reviews":  true,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "gh", "api",
		"--method", "PUT",
		"-H", "Accept: application/vnd.github+json",
		"--input", "-",
		"repos/"+owner+"/"+repo+"/actions/permissions/workflow",
	)
	cmd.Stdin = bytes.NewReader(payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("unable to allow Actions to create pull requests on %s/%s: %s", owner, repo, ghAPIError(out, err))
	}
	logf("Allowed GitHub Actions to create and approve pull requests on %s/%s", owner, repo)

	return nil
}

type actionsWorkflowPermissions struct {
	DefaultPermissions string `json:"default_workflow_permissions"`
	CanApprovePRs      bool   `json:"can_approve_pull_request_reviews"`
}

func getActionsWorkflowPermissions(ctx context.Context, owner, repo string) (actionsWorkflowPermissions, error) {
	cmd := exec.CommandContext(ctx, "gh", "api",
		"-H", "Accept: application/vnd.github+json",
		"repos/"+owner+"/"+repo+"/actions/permissions/workflow",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return actionsWorkflowPermissions{}, fmt.Errorf("unable to read Actions permissions for %s/%s: %s", owner, repo, ghAPIError(out, err))
	}
	var perms actionsWorkflowPermissions
	if err := json.Unmarshal(out, &perms); err != nil {
		return actionsWorkflowPermissions{}, fmt.Errorf("unable to parse Actions permissions for %s/%s: %w", owner, repo, err)
	}
	if perms.DefaultPermissions == "" {
		perms.DefaultPermissions = "read"
	}

	return perms, nil
}

// EnableBranchProtection turns on branch protection for the given branch with
// a pull-request requirement. Any existing protection is left as-is so the
// user's own review-count or status-check policies keep applying.
func EnableBranchProtection(
	ctx context.Context,
	owner, repo, branch string,
	logf func(string, ...any),
) error {
	if owner == "" || repo == "" {
		return errors.New("owner and repo are required to enable branch protection")
	}
	if branch == "" {
		branch = "main"
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("the GitHub CLI is not installed, so branch protection cannot be enabled")
	}

	protected, err := hasBranchProtection(ctx, owner, repo, branch)
	if err != nil {
		if isBranchProtectionUnavailable(err) {
			logf(
				"Branch protection is not available on this GitHub plan for %s/%s. "+
					"Make the repository public or upgrade to GitHub Pro to enable it.",
				owner, repo,
			)

			return nil
		}
		return err
	}
	if protected {
		logf("Branch %s on %s/%s is already protected, left as is", branch, owner, repo)

		return nil
	}

	body := map[string]any{
		"required_status_checks": nil,
		"enforce_admins":         false,
		"required_pull_request_reviews": map[string]any{
			"required_approving_review_count": 1,
		},
		"restrictions": nil,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "gh", "api",
		"--method", "PUT",
		"-H", "Accept: application/vnd.github+json",
		"--input", "-",
		"repos/"+owner+"/"+repo+"/branches/"+branch+"/protection",
	)
	cmd.Stdin = bytes.NewReader(payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("unable to protect branch %s on %s/%s: %s", branch, owner, repo, ghAPIError(out, err))
	}
	logf("Protected branch %s on %s/%s (require pull request before merging)", branch, owner, repo)

	return nil
}

func hasBranchProtection(ctx context.Context, owner, repo, branch string) (bool, error) {
	cmd := exec.CommandContext(ctx, "gh", "api",
		"repos/"+owner+"/"+repo+"/branches/"+branch+"/protection",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(out, []byte("HTTP 404")) ||
			bytes.Contains(out, []byte("Branch not protected")) ||
			bytes.Contains(out, []byte("Not Found")) {
			return false, nil
		}
		return false, fmt.Errorf("unable to read branch protection for %s/%s@%s: %s", owner, repo, branch, ghAPIError(out, err))
	}

	return true, nil
}

// isBranchProtectionUnavailable matches how GitHub reports that branch
// protection is off the plan for a private repository, so setup can list it
// as a step the user needs to take on a paid plan instead of failing.
func isBranchProtectionUnavailable(err error) bool {
	message := err.Error()

	return strings.Contains(message, "Upgrade to GitHub Pro") ||
		strings.Contains(message, "make this repository public")
}

// ghAPIError prefers the GitHub API's own error message over the exit status
// so the caller sees "HTTP 422: File already exists" rather than "exit 1".
func ghAPIError(out []byte, err error) string {
	message := strings.TrimSpace(string(out))
	if message == "" {
		return err.Error()
	}

	return strings.SplitN(message, "\n", 2)[0]
}

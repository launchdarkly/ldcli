package repository

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// ErrGitUnavailable reports that the Git executable cannot be used.
var ErrGitUnavailable = errors.New("git executable is unavailable")

// GitRepository identifies a repository discovered through Git.
type GitRepository struct {
	Root   string
	Origin string
}

type gitRunner interface {
	lookPath(name string) (string, error)
	output(dir string, args ...string) (stdout, stderr string, err error)
}

type execGit struct{}

// lookPath verifies that the Git executable is available.
func (execGit) lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// output executes Git in a working directory and returns trimmed output.
func (execGit) output(dir string, args ...string) (string, string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Repository discovery looks for the English "not a git repository"
	// message. A fixed locale keeps that message the same in every language.
	cmd.Env = append(os.Environ(), "LC_ALL=C")

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", strings.TrimSpace(string(exitErr.Stderr)), err
		}
		return "", "", err
	}

	return strings.TrimSpace(string(out)), "", nil
}

// FindGitRepository returns the Git repository containing dir.
func FindGitRepository(dir string) (GitRepository, bool, error) {
	return findGitRepository(execGit{}, dir)
}

// DeletedPaths returns staged and unstaged deleted paths relative to the repository.
func DeletedPaths(repositoryRoot string) ([]string, error) {
	return deletedPaths(execGit{}, repositoryRoot)
}

// findGitRepository contains the injectable repository-discovery workflow used
// by the real command and focused tests.
func findGitRepository(git gitRunner, dir string) (GitRepository, bool, error) {
	if _, err := git.lookPath("git"); err != nil {
		return GitRepository{}, false, fmt.Errorf("%w: %v", ErrGitUnavailable, err)
	}

	root, stderr, err := git.output(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if strings.Contains(strings.ToLower(stderr), "not a git repository") {
			return GitRepository{}, false, nil
		}
		if stderr != "" {
			return GitRepository{}, false, fmt.Errorf("find Git repository: %s: %w", stderr, err)
		}
		return GitRepository{}, false, fmt.Errorf("find Git repository: %w", err)
	}

	origin, stderr, err := git.output(root, "config", "--get", "remote.origin.url")
	if err != nil {
		if stderr == "" {
			return GitRepository{}, false, errors.New("Git origin is not configured")
		}
		return GitRepository{}, false, fmt.Errorf("read Git origin: %s: %w", stderr, err)
	}
	return GitRepository{Root: root, Origin: origin}, true, nil
}

func deletedPaths(git gitRunner, repositoryRoot string) ([]string, error) {
	var paths []string
	commands := [][]string{
		{"diff", "--name-only", "--diff-filter=D", "-z", "--", ".launchdarkly"},
		{"diff", "--cached", "--name-only", "--diff-filter=D", "-z", "--", ".launchdarkly"},
	}
	for _, command := range commands {
		output, stderr, err := git.output(repositoryRoot, command...)
		if err != nil {
			if stderr != "" {
				return nil, fmt.Errorf("find deleted sync files: %s: %w", stderr, err)
			}
			return nil, fmt.Errorf("find deleted sync files: %w", err)
		}
		paths = append(paths, splitNullTerminated(output)...)
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

func splitNullTerminated(value string) []string {
	var values []string
	for _, item := range strings.Split(value, "\x00") {
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}

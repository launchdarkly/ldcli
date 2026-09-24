package repository

import (
	"os/exec"
	"strings"
)

// GitRepository identifies a repository discovered through Git.
type GitRepository struct {
	Root string
}

type gitRunner interface {
	lookPath(name string) (string, error)
	output(dir string, args ...string) (string, error)
}

type execGit struct{}

// lookPath verifies that the Git executable is available.
func (execGit) lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// output executes Git in a working directory and returns trimmed stdout.
func (execGit) output(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}

// FindGitRepository returns the Git repository containing dir.
func FindGitRepository(dir string) (GitRepository, bool, error) {
	return findGitRepository(execGit{}, dir)
}

// findGitRepository contains the injectable repository-discovery workflow used
// by the real command and focused tests.
func findGitRepository(git gitRunner, dir string) (GitRepository, bool, error) {
	if _, err := git.lookPath("git"); err != nil {
		return GitRepository{}, false, nil
	}

	root, err := git.output(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return GitRepository{}, false, nil
	}

	return GitRepository{Root: root}, true, nil
}

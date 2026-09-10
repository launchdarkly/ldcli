package repository

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

var (
	ErrGitNotInstalled = errors.New("git is not installed; install git and initialize a repository before continuing")
	ErrNotGitRepo      = errors.New("not a git repository; run git init before continuing")
	ErrNoOrigin        = errors.New("repository has no origin remote; add a remote named origin before continuing")
)

type Repo struct {
	Root       string
	Identifier string
}

type gitRunner interface {
	lookPath(name string) (string, error)
	output(dir string, args ...string) (string, error)
}

type execGit struct{}

func (execGit) lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (execGit) output(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}

func IdentifyRepo(dir string) (Repo, error) {
	return identifyRepo(execGit{}, dir)
}

func identifyRepo(git gitRunner, dir string) (Repo, error) {
	if _, err := git.lookPath("git"); err != nil {
		return Repo{}, ErrGitNotInstalled
	}

	root, err := git.output(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, ErrNotGitRepo
	}

	origin, err := git.output(root, "config", "--local", "--get", "remote.origin.url")
	if err != nil || origin == "" {
		return Repo{}, ErrNoOrigin
	}

	identifier, err := repoIdentifier(origin)
	if err != nil {
		return Repo{}, err
	}

	return Repo{Root: root, Identifier: identifier}, nil
}

func repoIdentifier(remote string) (string, error) {
	repoPath := remote
	if parsed, err := url.Parse(remote); err == nil && parsed.Host != "" {
		repoPath = parsed.Path
	} else if _, path, ok := strings.Cut(remote, ":"); ok {
		repoPath = path
	}

	parts := strings.Split(strings.TrimSuffix(strings.Trim(repoPath, "/"), ".git"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("cannot derive repository identifier from origin %q", remote)
	}

	return strings.Join(parts[len(parts)-2:], "/"), nil
}

package sync

import (
	"errors"
	"fmt"
	"net"
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

	origin, err := git.output(root, "remote", "get-url", "origin")
	if err != nil || origin == "" {
		return Repo{}, ErrNoOrigin
	}

	id, err := normalizeRemoteURL(origin)
	if err != nil {
		return Repo{}, err
	}

	return Repo{Root: root, Identifier: id}, nil
}

func normalizeRemoteURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("origin remote URL is empty")
	}

	if !strings.Contains(s, "://") {
		host, path, ok := scpRemote(s)
		if !ok {
			return "", fmt.Errorf("invalid origin remote %q", raw)
		}

		return identifierFromHostPath(host, path)
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("invalid origin remote %q", raw)
	}

	return identifierFromHostPath(u.Hostname()+portSuffix(u.Port()), u.Path)
}

func portSuffix(port string) string {
	switch port {
	case "", "22", "443":
		return ""
	default:
		return ":" + port
	}
}

func scpRemote(s string) (host, path string, ok bool) {
	userHost, path, found := strings.Cut(s, ":")
	if !found || path == "" || strings.Contains(userHost, "/") {
		return "", "", false
	}

	_, host, found = strings.Cut(userHost, "@")
	if !found {
		host = userHost
	}

	return host, path, host != ""
}

func identifierFromHostPath(host, repoPath string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h + portSuffix(p)
	}

	repoPath = strings.Trim(strings.ToLower(repoPath), "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	repoPath = strings.Trim(repoPath, "/")

	if host == "" || repoPath == "" {
		return "", fmt.Errorf("invalid origin remote %q", host+"/"+repoPath)
	}

	return host + "/" + repoPath, nil
}

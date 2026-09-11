package repository

import (
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

type GitRepository struct {
	Root   string
	Source syncdomain.Source
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

func FindGitSource(dir string) (GitRepository, bool, error) {
	return findGitSource(execGit{}, dir)
}

func findGitSource(git gitRunner, dir string) (GitRepository, bool, error) {
	if _, err := git.lookPath("git"); err != nil {
		return GitRepository{}, false, nil
	}

	root, err := git.output(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return GitRepository{}, false, nil
	}

	origin, err := git.output(root, "config", "--local", "--get", "remote.origin.url")
	if err != nil || origin == "" {
		return GitRepository{}, false, nil
	}

	identifier, err := gitSourceIdentifier(origin)
	if err != nil {
		return GitRepository{}, false, err
	}

	source, err := syncdomain.NewSource(syncdomain.SourceTypeGit, identifier)
	if err != nil {
		return GitRepository{}, false, err
	}

	return GitRepository{Root: root, Source: source}, true, nil
}

func gitSourceIdentifier(remote string) (string, error) {
	remote = strings.TrimSpace(remote)

	var host, repoPath string

	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil || parsed.Host == "" {
			return "", invalidGitRemote(remote)
		}

		host = normalizedURLHost(parsed)
		repoPath = parsed.Path
	} else {
		remoteHost, remotePath, ok := strings.Cut(remote, ":")
		if !ok {
			return "", invalidGitRemote(remote)
		}

		if _, value, ok := strings.Cut(remoteHost, "@"); ok {
			remoteHost = value
		}

		host = strings.ToLower(strings.TrimSpace(remoteHost))
		repoPath = remotePath
	}

	repoPath = strings.TrimSuffix(strings.Trim(repoPath, "/"), ".git")
	if host == "" || !validRepositoryPath(repoPath) {
		return "", invalidGitRemote(remote)
	}

	return host + "/" + repoPath, nil
}

func normalizedURLHost(remote *url.URL) string {
	host := strings.ToLower(remote.Hostname())
	port := remote.Port()
	if port == "" || isDefaultPort(remote.Scheme, port) {
		return host
	}

	return net.JoinHostPort(host, port)
}

func isDefaultPort(scheme, port string) bool {
	switch strings.ToLower(scheme) {
	case "http":
		return port == "80"
	case "https":
		return port == "443"
	case "ssh":
		return port == "22"
	case "git":
		return port == "9418"
	default:
		return false
	}
}

func validRepositoryPath(repoPath string) bool {
	parts := strings.Split(repoPath, "/")
	if len(parts) < 2 {
		return false
	}

	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}

	return true
}

func invalidGitRemote(remote string) error {
	return fmt.Errorf("cannot derive source identifier from Git origin %q", remote)
}

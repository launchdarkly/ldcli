package awsdevops

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Sources reads the files of launchdarkly/ai-tooling, where the skill and the
// custom agent prompt live.
type Sources interface {
	// File returns a file of ai-tooling at ref, a branch or a commit SHA.
	File(ctx context.Context, ref, path string) (string, error)
}

const (
	aiToolingRepo   = "launchdarkly/ai-tooling"
	aiToolingBranch = "main"
	skillSourcePath = "skills/aws-devops-agent/experiment-orchestration/SKILL.md"
	agentSourcePath = "skills/aws-devops-agent/agents/experiment-orchestrator.md"
)

const (
	sourceTimeout = 30 * time.Second
	// maxSourceBytes caps a download. The files are a few KB.
	maxSourceBytes = 1 << 20
)

// GitHubSources reads ai-tooling from GitHub without authentication.
type GitHubSources struct {
	Client *http.Client
	RawURL string
}

// NewGitHubSources reads from github.com.
func NewGitHubSources() GitHubSources {
	return GitHubSources{
		Client: &http.Client{Timeout: sourceTimeout},
		RawURL: "https://raw.githubusercontent.com",
	}
}

func (s GitHubSources) File(ctx context.Context, ref, path string) (string, error) {
	body, err := s.get(ctx, fmt.Sprintf("%s/%s/%s/%s", s.RawURL, aiToolingRepo, url.PathEscape(ref), path))
	if err != nil {
		return "", fmt.Errorf("unable to download %s from %s at %s: %w", path, aiToolingRepo, ref, err)
	}

	return string(body), nil
}

func (s GitHubSources) get(ctx context.Context, address string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: sourceTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub sent %s", resp.Status)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes))
}

// fetchSources downloads the skill and the custom agent prompt from main.
func fetchSources(ctx context.Context, sources Sources) (skill, prompt string, err error) {
	if sources == nil {
		return "", "", fmt.Errorf("no source for the files of %s", aiToolingRepo)
	}
	if skill, err = sources.File(ctx, aiToolingBranch, skillSourcePath); err != nil {
		return "", "", err
	}
	if prompt, err = sources.File(ctx, aiToolingBranch, agentSourcePath); err != nil {
		return "", "", err
	}

	return skill, prompt, nil
}

package awsdevops

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Sources reads files from the main branch of launchdarkly/ai-tooling.
type Sources interface {
	File(ctx context.Context, path string) (string, error)
}

const (
	aiToolingRepo   = "launchdarkly/ai-tooling"
	skillSourcePath = "skills/aws-devops-agent/experiment-orchestration/SKILL.md"
	agentSourcePath = "skills/aws-devops-agent/agents/experiment-orchestrator.md"
	maxSourceBytes  = 1 << 20
)

type GitHubSources struct {
	Client *http.Client
	RawURL string
}

func NewGitHubSources() GitHubSources {
	return GitHubSources{
		Client: &http.Client{Timeout: 30 * time.Second},
		RawURL: "https://raw.githubusercontent.com",
	}
}

func (s GitHubSources) File(ctx context.Context, path string) (string, error) {
	text, err := s.get(ctx, fmt.Sprintf("%s/%s/main/%s", s.RawURL, aiToolingRepo, path))
	if err != nil {
		return "", fmt.Errorf("unable to download %s from %s: %w", path, aiToolingRepo, err)
	}

	return text, nil
}

func (s GitHubSources) get(ctx context.Context, address string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub sent %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes))

	return string(body), err
}

func fetchSources(ctx context.Context, sources Sources) (skill, prompt string, err error) {
	if sources == nil {
		return "", "", fmt.Errorf("no source for the files of %s", aiToolingRepo)
	}
	if skill, err = sources.File(ctx, skillSourcePath); err != nil {
		return "", "", err
	}
	if prompt, err = sources.File(ctx, agentSourcePath); err != nil {
		return "", "", err
	}

	return skill, prompt, nil
}

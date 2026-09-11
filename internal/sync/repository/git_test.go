package repository

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

func TestGitSourceIdentifier(t *testing.T) {
	tests := map[string]string{
		"git@github.com:launchdarkly/ldcli.git":                "github.com/launchdarkly/ldcli",
		"https://github.com/launchdarkly/ldcli.git":            "github.com/launchdarkly/ldcli",
		"https://github.com:443/launchdarkly/ldcli.git":        "github.com/launchdarkly/ldcli",
		"ssh://git@github.com/launchdarkly/ldcli.git":          "github.com/launchdarkly/ldcli",
		"ssh://git@git.example.com:2222/platform/team/service": "git.example.com:2222/platform/team/service",
	}

	for remote, expected := range tests {
		t.Run(remote, func(t *testing.T) {
			actual, err := gitSourceIdentifier(remote)
			require.NoError(t, err)
			assert.Equal(t, expected, actual)
		})
	}
}

func TestGitSourceIdentifierRejectsInvalidRemote(t *testing.T) {
	for _, remote := range []string{
		"",
		"ldcli",
		"file:///workspace/launchdarkly/ldcli",
		"https://github.com/ldcli",
		"https://github.com/org/../repo",
	} {
		t.Run(remote, func(t *testing.T) {
			_, err := gitSourceIdentifier(remote)
			require.Error(t, err)
		})
	}
}

func TestFindGitSource(t *testing.T) {
	repository, found, err := findGitSource(stubGit{
		commands: map[string]string{
			"rev-parse --show-toplevel":              "/workspace",
			"config --local --get remote.origin.url": "git@github.com:Acme/Widgets.git",
		},
	}, "/workspace/service")

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/workspace", repository.Root)
	assert.Equal(t, syncdomain.SourceTypeGit, repository.Source.Type())
	assert.Equal(t, "github.com/Acme/Widgets", repository.Source.Identifier())
}

func TestFindGitSourceFallsBackWhenGitIdentityIsUnavailable(t *testing.T) {
	tests := []struct {
		name string
		git  stubGit
	}{
		{
			name: "git is not installed",
			git:  stubGit{pathErr: errors.New("not found")},
		},
		{
			name: "workspace is not a repository",
			git: stubGit{
				errors: map[string]error{"rev-parse --show-toplevel": errors.New("not a repository")},
			},
		},
		{
			name: "repository has no origin",
			git: stubGit{
				commands: map[string]string{"rev-parse --show-toplevel": "/workspace"},
				errors: map[string]error{
					"config --local --get remote.origin.url": errors.New("missing"),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, found, err := findGitSource(test.git, "/workspace")

			require.NoError(t, err)
			assert.False(t, found)
			assert.Empty(t, repository)
		})
	}
}

func TestFindGitSourceRejectsInvalidOrigin(t *testing.T) {
	_, found, err := findGitSource(stubGit{
		commands: map[string]string{
			"rev-parse --show-toplevel":              "/workspace",
			"config --local --get remote.origin.url": "invalid",
		},
	}, "/workspace")

	require.Error(t, err)
	assert.False(t, found)
}

type stubGit struct {
	pathErr  error
	commands map[string]string
	errors   map[string]error
}

func (s stubGit) lookPath(string) (string, error) {
	if s.pathErr != nil {
		return "", s.pathErr
	}

	return "/usr/bin/git", nil
}

func (s stubGit) output(_ string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	if err, ok := s.errors[key]; ok {
		return "", err
	}
	if output, ok := s.commands[key]; ok {
		return output, nil
	}

	return "", errors.New("unexpected git " + key)
}

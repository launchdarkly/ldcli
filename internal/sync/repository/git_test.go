package repository

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindGitRepository(t *testing.T) {
	git := &fakeGit{
		path: "/usr/bin/git",
		outputs: map[string]gitResult{
			"rev-parse --show-toplevel": {output: "/tmp/example"},
		},
	}

	repository, found, err := findGitRepository(git, "/tmp/example/subdirectory")

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "/tmp/example", repository.Root)
}

func TestFindGitRepositoryDoesNotRequireOrigin(t *testing.T) {
	git := &fakeGit{
		path: "/usr/bin/git",
		outputs: map[string]gitResult{
			"rev-parse --show-toplevel": {output: "/tmp/local-only"},
		},
	}

	repository, found, err := findGitRepository(git, "/tmp/local-only")

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "/tmp/local-only", repository.Root)
}

func TestFindGitRepositoryRequiresGitAndInitializedRepository(t *testing.T) {
	t.Run("git executable missing", func(t *testing.T) {
		_, found, err := findGitRepository(&fakeGit{pathErr: errors.New("missing")}, ".")
		require.NoError(t, err)
		require.False(t, found)
	})

	t.Run("repository missing", func(t *testing.T) {
		git := &fakeGit{
			path: "/usr/bin/git",
			outputs: map[string]gitResult{
				"rev-parse --show-toplevel": {err: errors.New("not a repository")},
			},
		}
		_, found, err := findGitRepository(git, ".")
		require.NoError(t, err)
		require.False(t, found)
	})
}

type gitResult struct {
	output string
	err    error
}

type fakeGit struct {
	path    string
	pathErr error
	outputs map[string]gitResult
}

func (git *fakeGit) lookPath(string) (string, error) {
	return git.path, git.pathErr
}

func (git *fakeGit) output(_ string, args ...string) (string, error) {
	result := git.outputs[joinArgs(args)]
	return result.output, result.err
}

func joinArgs(args []string) string {
	var joined string
	for index, arg := range args {
		if index != 0 {
			joined += " "
		}
		joined += arg
	}
	return joined
}

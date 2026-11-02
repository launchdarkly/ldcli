package reference_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdapterFixturesHaveNoToolLoadedInstructionPaths(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "internal", "sync")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		require.NoError(t, err)
		relative, err := filepath.Rel(root, path)
		require.NoError(t, err)
		relative = filepath.ToSlash(relative)
		if !insideTestdata(relative) {
			return nil
		}
		require.Falsef(t, forbiddenInstructionPath(relative), "forbidden adapter fixture path: %s", relative)
		return nil
	})
	require.NoError(t, err)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		require.NotEqual(t, directory, parent, "repository root was not found")
		directory = parent
	}
}

func forbiddenInstructionPath(path string) bool {
	parts := strings.Split(path, "/")
	for _, part := range parts {
		switch part {
		case ".cursor", ".claude", ".agents", ".cursorrules", "AGENTS.md", "CLAUDE.md", "SKILL.md":
			return true
		}
	}
	return path == ".github/copilot-instructions.md" || strings.HasPrefix(path, ".github/instructions/")
}

func insideTestdata(path string) bool {
	for _, part := range strings.Split(path, "/") {
		if part == "testdata" {
			return true
		}
	}
	return false
}

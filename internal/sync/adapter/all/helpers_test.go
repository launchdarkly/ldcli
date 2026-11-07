package all

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/adaptertest"
)

type frameworkCase struct {
	name, fixture, format, collection, id, sourcePath, want string
	kind                                                    syncdomain.Kind
}

type externalFakeAdapter struct{}

func (externalFakeAdapter) Format() string { return "external-fake" }

func (externalFakeAdapter) Behavior() string { return "external-fake/1" }

func (externalFakeAdapter) Inspect(bundle adapter.SourceBundle) (*adapter.Node, error) {
	document := bundle.Primary
	return &adapter.Node{
		Value: string(document.Data), Start: 0, End: len(document.Data),
		Capability: syncdomain.CapabilityDirect,
		Encode:     func(value any) ([]byte, error) { return []byte(value.(string)), nil },
	}, nil
}

func (externalFakeAdapter) Defaults(syncdomain.Kind) (string, map[string]string) {
	return "$", map[string]string{"prompt": "$"}
}

// fixturePackages maps the first path segment of a fixture name to the
// package that owns the fixture.
var fixturePackages = map[string]string{
	"python": "python", "typescript": "ecmascript", "javascript": "ecmascript",
	"json": "structured", "yaml": "structured", "toml": "structured",
	"markdown": "markdown", "profiles": "markdown/profiles", "agent-skill": "markdown/agent-skill",
}

// fixtureFile reads a fixture of one adapter package by its old shared name,
// such as python/agents.py.
func fixtureFile(t *testing.T, name string) []byte {
	t.Helper()
	first, rest, _ := strings.Cut(name, "/")
	if first == "imports" {
		language, file, _ := strings.Cut(rest, "/")
		first, rest = language, "imports/"+file
	}
	directory, ok := fixturePackages[first]
	require.True(t, ok, "unknown fixture %q", name)
	parts := strings.SplitN(directory, "/", 2)
	path := filepath.Join("..", parts[0], "testdata")
	if len(parts) == 2 {
		path = filepath.Join(path, parts[1])
	}
	data, err := os.ReadFile(filepath.Join(path, filepath.FromSlash(rest)))
	require.NoError(t, err)
	return data
}

func materializeFixture(t *testing.T, fixture, realPath string) (string, []byte) {
	t.Helper()
	data := fixtureFile(t, fixture)
	return adaptertest.Materialize(t, data, realPath), data
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

package wizard_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/wizard"
)

func TestDetectStack(t *testing.T) {
	t.Run("detects React from package.json with react dependency", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0"}}`)

		ids := wizard.DetectStack(dir)

		require.NotEmpty(t, ids)
		assert.Equal(t, "react-client-sdk", ids[0])
	})

	t.Run("detects Next.js as node-server from package.json", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0","next":"^14.0.0"}}`)

		ids := wizard.DetectStack(dir)

		require.NotEmpty(t, ids)
		assert.Equal(t, "node-server", ids[0])
	})

	t.Run("detects Node.js from package.json without react", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "package.json", `{"dependencies":{"express":"^4.0.0"}}`)

		ids := wizard.DetectStack(dir)

		require.NotEmpty(t, ids)
		assert.Equal(t, "node-server", ids[0])
	})

	t.Run("detects Go from go.mod", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", "module example.com/myapp\n\ngo 1.21\n")

		ids := wizard.DetectStack(dir)

		require.NotEmpty(t, ids)
		assert.Equal(t, "go-server-sdk", ids[0])
	})

	t.Run("detects Python from requirements.txt", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "requirements.txt", "flask==3.0.0\n")

		ids := wizard.DetectStack(dir)

		require.NotEmpty(t, ids)
		assert.Equal(t, "python-server-sdk", ids[0])
	})

	t.Run("detects Python from pyproject.toml", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "pyproject.toml", "[tool.poetry]\nname = \"myapp\"\n")

		ids := wizard.DetectStack(dir)

		require.NotEmpty(t, ids)
		assert.Equal(t, "python-server-sdk", ids[0])
	})

	t.Run("detects Java from pom.xml", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "pom.xml", "<project></project>")

		ids := wizard.DetectStack(dir)

		require.NotEmpty(t, ids)
		assert.Equal(t, "java-server-sdk", ids[0])
	})

	t.Run("detects Java from build.gradle", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "build.gradle", "plugins { id 'java' }")

		ids := wizard.DetectStack(dir)

		require.NotEmpty(t, ids)
		assert.Equal(t, "java-server-sdk", ids[0])
	})

	t.Run("returns nil for unrecognised project", func(t *testing.T) {
		dir := t.TempDir()

		ids := wizard.DetectStack(dir)

		assert.Empty(t, ids)
	})

	t.Run("returns multiple results when multiple indicators are present", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "package.json", `{"dependencies":{"react":"^18.0.0"}}`)
		writeFile(t, dir, "requirements.txt", "flask==3.0.0\n")

		ids := wizard.DetectStack(dir)

		assert.Contains(t, ids, "react-client-sdk")
		assert.Contains(t, ids, "python-server-sdk")
	})
}

func TestDetectPackageManager(t *testing.T) {
	t.Run("detects pnpm from pnpm-lock.yaml", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "pnpm-lock.yaml", "")

		assert.Equal(t, "pnpm", wizard.DetectPackageManager(dir))
	})

	t.Run("detects yarn from yarn.lock", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "yarn.lock", "")

		assert.Equal(t, "yarn", wizard.DetectPackageManager(dir))
	})

	t.Run("defaults to npm when no lock file is found", func(t *testing.T) {
		dir := t.TempDir()

		assert.Equal(t, "npm", wizard.DetectPackageManager(dir))
	})

	t.Run("prefers pnpm over yarn when both are present", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "pnpm-lock.yaml", "")
		writeFile(t, dir, "yarn.lock", "")

		assert.Equal(t, "pnpm", wizard.DetectPackageManager(dir))
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600)
	require.NoError(t, err)
}

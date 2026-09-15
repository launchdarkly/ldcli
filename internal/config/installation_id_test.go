package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestEnsureInstallationIDCreatesConfig(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "ldcli", "config.yml")

	installationID, err := EnsureInstallationID(filename)

	require.NoError(t, err)
	require.NoError(t, uuid.Validate(installationID))

	loaded, err := New(filename, os.ReadFile)
	require.NoError(t, err)
	assert.Equal(t, installationID, loaded.InstallationID)

	info, err := os.Stat(filename)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestEnsureInstallationIDIsStableAndPreservesConfig(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(
		filename,
		[]byte("access-token: token\nfuture-setting: value\n"),
		0o640,
	))

	first, err := EnsureInstallationID(filename)
	require.NoError(t, err)
	second, err := EnsureInstallationID(filename)
	require.NoError(t, err)
	assert.Equal(t, first, second)

	data, err := os.ReadFile(filename)
	require.NoError(t, err)

	var values map[string]any
	require.NoError(t, yaml.Unmarshal(data, &values))
	assert.Equal(t, "token", values["access-token"])
	assert.Equal(t, "value", values["future-setting"])
	assert.Equal(t, first, values[installationIDKey])

	info, err := os.Stat(filename)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestEnsureInstallationIDUsesExistingValueWithoutRewriting(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	const existing = "installation-id: 45cb6eca-6c83-4db6-b171-174fd2fed588\n"
	require.NoError(t, os.WriteFile(filename, []byte(existing), 0o600))

	installationID, err := EnsureInstallationID(filename)

	require.NoError(t, err)
	assert.Equal(t, "45cb6eca-6c83-4db6-b171-174fd2fed588", installationID)

	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	assert.Equal(t, existing, string(data))
}

func TestEnsureInstallationIDRejectsInvalidValue(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(filename, []byte("installation-id: invalid\n"), 0o600))

	_, err := EnsureInstallationID(filename)

	require.ErrorContains(t, err, "installation ID")
}

func TestConfigJSONDoesNotExposeInstallationID(t *testing.T) {
	data, err := json.Marshal(Config{InstallationID: uuid.NewString()})

	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(data))
}

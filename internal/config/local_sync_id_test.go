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

func TestEnsureLocalSyncIDCreatesConfig(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "ldcli", "config.yml")

	localSyncID, err := EnsureLocalSyncID(filename)

	require.NoError(t, err)
	require.NoError(t, uuid.Validate(localSyncID))

	loaded, err := New(filename, os.ReadFile)
	require.NoError(t, err)
	assert.Equal(t, localSyncID, loaded.LocalSyncID)

	info, err := os.Stat(filename)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestEnsureLocalSyncIDIsStableAndPreservesConfig(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(
		filename,
		[]byte("access-token: token\nfuture-setting: value\n"),
		0o640,
	))

	first, err := EnsureLocalSyncID(filename)
	require.NoError(t, err)
	second, err := EnsureLocalSyncID(filename)
	require.NoError(t, err)
	assert.Equal(t, first, second)

	data, err := os.ReadFile(filename)
	require.NoError(t, err)

	var values map[string]any
	require.NoError(t, yaml.Unmarshal(data, &values))
	assert.Equal(t, "token", values["access-token"])
	assert.Equal(t, "value", values["future-setting"])
	assert.Equal(t, first, values[localSyncIDKey])

	info, err := os.Stat(filename)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func TestEnsureLocalSyncIDUsesExistingValueWithoutRewriting(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	const existing = "local-sync-id: 45cb6eca-6c83-4db6-b171-174fd2fed588\n"
	require.NoError(t, os.WriteFile(filename, []byte(existing), 0o600))

	localSyncID, err := EnsureLocalSyncID(filename)

	require.NoError(t, err)
	assert.Equal(t, "45cb6eca-6c83-4db6-b171-174fd2fed588", localSyncID)

	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	assert.Equal(t, existing, string(data))
}

func TestEnsureLocalSyncIDRejectsInvalidValue(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, os.WriteFile(filename, []byte("local-sync-id: invalid\n"), 0o600))

	_, err := EnsureLocalSyncID(filename)

	require.ErrorContains(t, err, "local sync ID")
}

func TestConfigJSONDoesNotExposeLocalSyncID(t *testing.T) {
	data, err := json.Marshal(Config{LocalSyncID: uuid.NewString()})

	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(data))
}

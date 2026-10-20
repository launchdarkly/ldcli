package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeatbeltProfile_DeniesHomeThenOpensFolders(t *testing.T) {
	profile := seatbeltProfile(sandboxPaths{
		Home:      "/Users/me",
		ReadWrite: []string{"/Users/me/app"},
		ReadOnly:  []string{`/Users/me/odd "name"`},
	})

	deny := strings.Index(profile, `(deny file-read* file-write* (subpath "/Users/me"))`)
	allow := strings.Index(profile, `(allow file-read* file-write* (subpath "/Users/me/app"))`)
	require.NotEqual(t, -1, deny)
	require.NotEqual(t, -1, allow)
	assert.Less(t, deny, allow, "later rules win, so the allows must follow the deny")
	assert.Contains(t, profile, `(allow file-read* (subpath "/Users/me/odd \"name\""))`, "paths are quoted")
}

func TestBuildSandboxPaths_OpensOnlyOpenCodesXDGFolders(t *testing.T) {
	p := buildSandboxPaths("/Users/me", "/Users/me/app", "", []string{"XDG_CONFIG_HOME=/Users/me/.ldcli-local"})

	assert.Contains(t, p.ReadWrite, "/Users/me/app")
	assert.Contains(t, p.ReadWrite, "/Users/me/.local/share/opencode")
	assert.Contains(t, p.ReadWrite, "/Users/me/.local/share/uv", "uv writes when it inspects a managed Python")
	assert.Contains(t, p.ReadOnly, "/Users/me/.ldcli-local/opencode")
	assert.NotContains(t, p.ReadOnly, "/Users/me/.ldcli-local", "the ldcli configuration next to it stays closed")
	for _, path := range append(p.ReadOnly, p.ReadWrite...) {
		assert.NotEqual(t, "/Users/me/.config", path)
		assert.NotEqual(t, "/Users/me/.config/ldcli", path)
	}
}

func TestInstallDir(t *testing.T) {
	assert.Equal(t, "/opt/homebrew/lib/node_modules/opencode-ai", installDir("/Users/me", "/opt/homebrew/lib/node_modules/opencode-ai/bin/opencode.exe"))
	assert.Equal(t, "/Users/me/.opencode", installDir("/Users/me", "/Users/me/.opencode/bin/opencode"))
	assert.Equal(t, "/Users/me/.local/bin", installDir("/Users/me", "/Users/me/.local/bin/opencode"), "a shared bin folder opens only itself")
	assert.Equal(t, "/usr/local/bin", installDir("/Users/me", "/usr/local/bin/opencode"))
	assert.Equal(t, "/bin", installDir("/Users/me", "/bin/opencode"), "never open / or another folder that holds the home folder")
	assert.Equal(t, "/Users/bin", installDir("/Users/me", "/Users/bin/opencode"))
}

func TestOpenCodeConfig_DeniesLDCLIAndSkillsInstalls(t *testing.T) {
	data, err := openCodeConfig()
	require.NoError(t, err)

	assert.Contains(t, string(data), `"ldcli*":"deny"`)
	assert.Contains(t, string(data), `"npx skills*":"deny"`)
}

// TestSandboxCommand_ConfinesReadsToTheProject runs a real sandboxed command on
// macOS, with a fake home folder.
func TestSandboxCommand_ConfinesReadsToTheProject(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the sandbox is macOS only")
	}
	if _, err := os.Stat(sandboxExec); err != nil {
		t.Skip("sandbox-exec is not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "code", "app")
	write := func(path, content string) string {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}
	inside := write(filepath.Join(project, "main.py"), "inside")
	secrets := []string{
		write(filepath.Join(home, ".config", "ldcli", "config.yml"), "access-token: api-secret"),
		write(filepath.Join(home, ".ssh", "id_ed25519"), "key"),
		write(filepath.Join(home, "code", "other-repo", "main.py"), "other"),
	}

	run := func(name string, args ...string) (string, error) {
		box, err := sandboxCommand(name, args, project, os.Environ())
		require.NoError(t, err)
		defer box.Cleanup()
		require.Equal(t, sandboxExec, box.Name)
		cmd := exec.Command(box.Name, box.Args...)
		cmd.Env = append(os.Environ(), "TMPDIR="+box.TempDir)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run("/bin/cat", inside)
	require.NoError(t, err, out)
	assert.Equal(t, "inside", out)
	for _, path := range secrets {
		out, err := run("/bin/cat", path)
		assert.Error(t, err, "read %s", path)
		assert.Contains(t, out, "Operation not permitted", path)
	}

	out, err = run("/usr/bin/touch", filepath.Join(project, "new.txt"))
	require.NoError(t, err, out)
	assert.FileExists(t, filepath.Join(project, "new.txt"), "the project stays writable")

	// Resolving a path reads the metadata of each parent folder, which uv and
	// git need.
	out, err = run("/bin/realpath", inside)
	require.NoError(t, err, out)
	assert.Contains(t, out, "main.py")

	// The shared temporary folder is closed, and the run's own one is open.
	other, err := os.MkdirTemp(sharedTemp, "ldcli-sandbox-test-")
	require.NoError(t, err)
	defer os.RemoveAll(other)
	otherFile := write(filepath.Join(other, "notes.txt"), "another program's file")
	out, err = run("/bin/cat", otherFile)
	assert.Error(t, err)
	assert.Contains(t, out, "Operation not permitted")
	out, err = run("/bin/sh", "-c", `echo ok > "$TMPDIR/scratch" && cat "$TMPDIR/scratch"`)
	require.NoError(t, err, out)
	assert.Equal(t, "ok\n", out)
}

func TestSandboxCommand_RemovesTheRunTempFolder(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the sandbox is macOS only")
	}
	if _, err := os.Stat(sandboxExec); err != nil {
		t.Skip("sandbox-exec is not available")
	}
	box, err := sandboxCommand("/bin/true", nil, t.TempDir(), os.Environ())
	require.NoError(t, err)
	require.DirExists(t, box.TempDir)

	box.Cleanup()

	assert.NoDirExists(t, box.TempDir)
}

func TestSeatbeltProfile_OpensParentMetadataOnly(t *testing.T) {
	profile := seatbeltProfile(sandboxPaths{
		Home:      "/Users/me",
		Temp:      "/private/tmp",
		RunTemp:   "/private/tmp/ldcli-agent-run-1",
		ReadWrite: []string{"/Users/me/code/app"},
	})

	assert.Contains(t, profile, `(allow file-read-metadata (literal "/Users/me/code"))`)
	assert.NotContains(t, profile, `(allow file-read* (subpath "/Users/me/code"))`, "the parent's contents stay closed")
	assert.Contains(t, profile, `(deny file-read* file-write* (subpath "/private/tmp"))`)
	assert.Contains(t, profile, `(allow file-read* file-write* (subpath "/private/tmp/ldcli-agent-run-1"))`)
}

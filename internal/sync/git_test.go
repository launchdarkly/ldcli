package sync

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRemoteURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "git@github.com:Acme/Widgets.git", want: "github.com/acme/widgets"},
		{in: "https://github.com/Acme/Widgets.git", want: "github.com/acme/widgets"},
		{in: "https://github.com/Acme/Widgets", want: "github.com/acme/widgets"},
		{in: "https://github.com/Acme/Widgets/", want: "github.com/acme/widgets"},
		{in: "ssh://git@github.com/Acme/Widgets.git", want: "github.com/acme/widgets"},
		{in: "https://user:token@github.com/Acme/Widgets.git", want: "github.com/acme/widgets"},
		{in: "https://github.com:443/Acme/Widgets.git", want: "github.com/acme/widgets"},
		{in: "ssh://git@github.com:22/Acme/Widgets.git", want: "github.com/acme/widgets"},
		{in: "https://ghe.example.com:8443/Acme/Widgets.git", want: "ghe.example.com:8443/acme/widgets"},
		{in: "https://gitlab.com/group/sub/repo.git", want: "gitlab.com/group/sub/repo"},
		{in: "org-123@github.com:Acme/Widgets.git", want: "github.com/acme/widgets"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := normalizeRemoteURL(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeRemoteURL_RejectsInvalid(t *testing.T) {
	for _, in := range []string{"", "   ", "not a remote", "://github.com/acme/widgets"} {
		_, err := normalizeRemoteURL(in)
		require.Error(t, err, in)
	}
}

func TestIdentifyRepo(t *testing.T) {
	requireGit(t)

	dir := initGitRepo(t)
	runGit(t, dir, "remote", "add", "origin", "git@github.com:Acme/Widgets.git")

	repo, err := IdentifyRepo(dir)
	require.NoError(t, err)
	assert.Equal(t, "github.com/acme/widgets", repo.Identifier)
	assert.Equal(t, absPath(t, dir), absPath(t, repo.Root))
}

func TestIdentifyRepo_HTTPSMatchesSSH(t *testing.T) {
	requireGit(t)

	sshDir := initGitRepo(t)
	runGit(t, sshDir, "remote", "add", "origin", "git@github.com:Acme/Widgets.git")

	httpsDir := initGitRepo(t)
	runGit(t, httpsDir, "remote", "add", "origin", "https://github.com/Acme/Widgets.git")

	sshRepo, err := IdentifyRepo(sshDir)
	require.NoError(t, err)
	httpsRepo, err := IdentifyRepo(httpsDir)
	require.NoError(t, err)

	assert.Equal(t, sshRepo.Identifier, httpsRepo.Identifier)
}

func TestIdentifyRepo_FromSubdirectory(t *testing.T) {
	requireGit(t)

	dir := initGitRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://github.com/Acme/Widgets.git")

	nested := filepath.Join(dir, "apps", "api")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	repo, err := IdentifyRepo(nested)
	require.NoError(t, err)
	assert.Equal(t, "github.com/acme/widgets", repo.Identifier)
	assert.Equal(t, absPath(t, dir), absPath(t, repo.Root))
}

func TestIdentifyRepo_StableAcrossBranches(t *testing.T) {
	requireGit(t)

	dir := initGitRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://github.com/Acme/Widgets.git")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0o644))
	runGit(t, dir, "add", "README")
	runGit(t, dir, "commit", "--quiet", "-m", "init")

	main, err := IdentifyRepo(dir)
	require.NoError(t, err)

	runGit(t, dir, "checkout", "-b", "feature")

	feature, err := IdentifyRepo(dir)
	require.NoError(t, err)
	assert.Equal(t, main.Identifier, feature.Identifier)
}

func TestIdentifyRepo_NotARepo(t *testing.T) {
	requireGit(t)

	_, err := IdentifyRepo(t.TempDir())
	require.ErrorIs(t, err, ErrNotGitRepo)
}

func TestIdentifyRepo_NoOrigin(t *testing.T) {
	requireGit(t)

	_, err := IdentifyRepo(initGitRepo(t))
	require.ErrorIs(t, err, ErrNoOrigin)
}

func TestIdentifyRepo_GitNotInstalled(t *testing.T) {
	_, err := identifyRepo(stubGit{pathErr: errors.New("not found")}, t.TempDir())
	require.ErrorIs(t, err, ErrGitNotInstalled)
}

func TestIdentifyRepo_StubNotARepo(t *testing.T) {
	_, err := identifyRepo(stubGit{
		errs: map[string]error{"rev-parse --show-toplevel": errors.New("fatal")},
	}, "/tmp/proj")
	require.ErrorIs(t, err, ErrNotGitRepo)
}

func TestIdentifyRepo_StubNoOrigin(t *testing.T) {
	_, err := identifyRepo(stubGit{
		cmds: map[string]string{"rev-parse --show-toplevel": "/repo"},
		errs: map[string]error{"remote get-url origin": errors.New("no such remote")},
	}, "/repo")
	require.ErrorIs(t, err, ErrNoOrigin)
}

func TestIdentifyRepo_StubOrigin(t *testing.T) {
	repo, err := identifyRepo(stubGit{
		cmds: map[string]string{
			"rev-parse --show-toplevel": "/repo",
			"remote get-url origin":     "https://github.com/Acme/Widgets.git",
		},
	}, "/repo")
	require.NoError(t, err)
	assert.Equal(t, "/repo", repo.Root)
	assert.Equal(t, "github.com/acme/widgets", repo.Identifier)
}

type stubGit struct {
	pathErr error
	cmds    map[string]string
	errs    map[string]error
}

func (s stubGit) lookPath(string) (string, error) {
	if s.pathErr != nil {
		return "", s.pathErr
	}

	return "/usr/bin/git", nil
}

func (s stubGit) output(_ string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	if s.errs != nil {
		if err, ok := s.errs[key]; ok {
			return "", err
		}
	}
	if s.cmds != nil {
		if out, ok := s.cmds[key]; ok {
			return out, nil
		}
	}

	return "", errors.New("unexpected git " + key)
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{
		"-c", "user.name=ldcli",
		"-c", "user.email=ldcli@example.com",
	}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func absPath(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	return resolved
}

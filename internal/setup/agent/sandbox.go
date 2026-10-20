package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The setup agent runs commands without approvals, and OpenCode's rules limit
// only its file tools, not the paths that a shell command touches. So on
// macOS, ldcli starts OpenCode in a Seatbelt sandbox (sandbox-exec), which
// applies to OpenCode and to every command that it runs. The sandbox denies
// the user's home folder, except the project, OpenCode's own folders, package
// caches, and toolchains. Other repos, ~/.config/ldcli, and ~/.ssh stay
// unreadable. System folders and the network stay open, because the model API
// and the package registries need them.

const sandboxExec = "/usr/bin/sandbox-exec"

// sandboxPaths are the folders that the sandbox closes and opens.
type sandboxPaths struct {
	Home string
	// Temp is the shared temporary folder, /private/tmp. Other programs keep
	// files there, so the sandbox closes it. RunTemp is the run's own
	// temporary folder inside it, and it stays open.
	Temp    string
	RunTemp string
	// ReadWrite are folders that the agent and its commands can change.
	ReadWrite []string
	// ReadOnly are folders that commands must read but not change.
	ReadOnly []string
}

// readWriteUnderHome are package caches and OpenCode's own folders, relative to
// the home folder. A package install writes to its cache, and OpenCode writes
// its sessions and snapshots.
var readWriteUnderHome = []string{
	".local/share/opencode",
	".local/state/opencode",
	// uv writes here when it inspects or installs a managed Python.
	".local/share/uv",
	".cache/opencode",
	".cache/uv",
	".cache/pip",
	".cache/pypoetry",
	".cache/go-build",
	".npm",
	".bun/install/cache",
	".gradle",
	".m2",
	".cargo/registry",
	".cargo/git",
	"go/pkg/mod",
	"Library/Caches/pip",
	"Library/Caches/pypoetry",
	"Library/Caches/go-build",
	"Library/Caches/Yarn",
	"Library/Caches/pnpm",
	"Library/pnpm",
}

// readOnlyUnderHome are toolchains and settings that commands read.
var readOnlyUnderHome = []string{
	".config/opencode",
	".local/bin",
	".local/share/mise",
	".pyenv",
	".rbenv",
	".asdf",
	".nvm",
	".volta",
	".bun/bin",
	".deno",
	".rustup",
	".cargo/bin",
	"go/bin",
	".sdkman",
	".gitconfig",
	".config/git",
	// Go reads user settings, such as GOPROXY and GOPRIVATE, from this file.
	"Library/Application Support/go/env",
}

// buildSandboxPaths returns the folders to open for a run in dir. opencodeDir
// is the folder that holds the OpenCode program, so a copy installed under
// the home folder can still start.
func buildSandboxPaths(home, dir, opencodeDir string, env []string) sandboxPaths {
	p := sandboxPaths{Home: home, ReadWrite: []string{dir}}
	for _, rel := range readWriteUnderHome {
		p.ReadWrite = append(p.ReadWrite, filepath.Join(home, rel))
	}
	for _, rel := range readOnlyUnderHome {
		p.ReadOnly = append(p.ReadOnly, filepath.Join(home, rel))
	}
	// OpenCode follows the XDG folder variables. If they point elsewhere,
	// open only OpenCode's folder in each.
	for name, mode := range map[string]string{"XDG_DATA_HOME": "rw", "XDG_STATE_HOME": "rw", "XDG_CACHE_HOME": "rw", "XDG_CONFIG_HOME": "ro"} {
		value := envValue(env, name)
		if value == "" {
			continue
		}
		folder := filepath.Join(value, "opencode")
		if mode == "rw" {
			p.ReadWrite = append(p.ReadWrite, folder)
		} else {
			p.ReadOnly = append(p.ReadOnly, folder)
		}
	}
	// GOENV moves the Go settings file. The value "off" turns the file off.
	if goenv := envValue(env, "GOENV"); goenv != "" && goenv != "off" {
		p.ReadOnly = append(p.ReadOnly, goenv)
	}
	if opencodeDir != "" {
		p.ReadOnly = append(p.ReadOnly, opencodeDir)
	}
	return p
}

// seatbeltProfile returns the sandbox profile. Later rules take precedence,
// so the allow rules for the open folders come after the deny rules.
func seatbeltProfile(p sandboxPaths) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	fmt.Fprintf(&b, "(deny file-read* file-write* (subpath %s))\n", sbString(p.Home))
	if p.Temp != "" {
		fmt.Fprintf(&b, "(deny file-read* file-write* (subpath %s))\n", sbString(p.Temp))
		fmt.Fprintf(&b, "(allow file-read-metadata (literal %s))\n", sbString(p.Temp))
	}
	// Resolving a path, as realpath, uv, and git do, reads the metadata of
	// every parent folder. So the parents of each open folder allow metadata
	// reads, but not their contents.
	for _, parent := range parentFolders(p) {
		fmt.Fprintf(&b, "(allow file-read-metadata (literal %s))\n", sbString(parent))
	}
	if p.RunTemp != "" {
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", sbString(p.RunTemp))
	}
	for _, path := range p.ReadOnly {
		fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", sbString(path))
	}
	for _, path := range p.ReadWrite {
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", sbString(path))
	}
	return b.String()
}

// parentFolders returns the closed folders between each open folder and the
// home folder, the home folder included, without repeats.
func parentFolders(p sandboxPaths) []string {
	seen := map[string]bool{}
	var out []string
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	add(p.Home)
	for _, path := range append(append([]string{}, p.ReadWrite...), p.ReadOnly...) {
		if !strings.HasPrefix(path, p.Home+"/") {
			continue
		}
		for dir := filepath.Dir(path); dir != p.Home && strings.HasPrefix(dir, p.Home+"/"); dir = filepath.Dir(dir) {
			add(dir)
		}
	}
	return out
}

// sbString quotes a path for a Seatbelt profile.
func sbString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func envValue(env []string, name string) string {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			return v
		}
	}
	return ""
}

// installDir returns the folder to open for the OpenCode program at path. The
// program is often a launcher in the bin folder of its own install, such as
// .../node_modules/opencode-ai/bin, so the folder above bin holds the rest of
// the install. A shared bin folder, such as ~/.local/bin, opens only itself,
// so other programs' data stays closed.
func installDir(home, path string) string {
	bin := filepath.Dir(path)
	if filepath.Base(bin) != "bin" {
		return bin
	}
	root := filepath.Dir(bin)
	shared := map[string]bool{
		home:                          true,
		filepath.Join(home, ".local"): true,
		"/usr":                        true,
		"/usr/local":                  true,
		"/opt/homebrew":               true,
	}
	// Never open a folder that holds the home folder, such as / or /Users.
	containsHome := root == home || strings.HasPrefix(home, strings.TrimSuffix(root, "/")+"/")
	if shared[root] || containsHome {
		return bin
	}
	return root
}

// realPath resolves the symlinks in path. A path that does not exist yet keeps
// its resolved parent, so a cache folder that a command creates later is
// still covered.
func realPath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(realPath(parent), filepath.Base(path))
}

func mapPaths(paths []string, f func(string) string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = f(p)
	}
	return out
}

// sandbox is how one run starts OpenCode.
type sandbox struct {
	// Name and Args start the program.
	Name string
	Args []string
	// TempDir, if set, is the run's own temporary folder. OpenCode must get
	// it as TMPDIR, because the shared temporary folder is closed.
	TempDir string
	// Cleanup removes the profile and the run's temporary folder.
	Cleanup func()
}

// sandboxCommand returns how to run binary with args in the sandbox. If the
// sandbox is not available, it runs binary and args unchanged.
func sandboxCommand(binary string, args []string, dir string, env []string) (sandbox, error) {
	plain := sandbox{Name: binary, Args: args, Cleanup: func() {}}
	if runtime.GOOS != "darwin" {
		return plain, nil
	}
	if _, err := os.Stat(sandboxExec); err != nil {
		return plain, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return sandbox{}, fmt.Errorf("could not find the home folder for the agent sandbox: %w", err)
	}
	opencodeDir := ""
	if path, err := exec.LookPath(binary); err == nil {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			opencodeDir = installDir(home, real)
		}
	}
	// Seatbelt matches the real path, so a folder behind a symlink, such as
	// /var/folders on macOS, must be written as the folder it points to.
	paths := buildSandboxPaths(realPath(home), realPath(dir), opencodeDir, env)
	paths.ReadWrite = mapPaths(paths.ReadWrite, realPath)
	paths.ReadOnly = mapPaths(paths.ReadOnly, realPath)
	runTemp, err := os.MkdirTemp(sharedTemp, "ldcli-agent-run-")
	if err != nil {
		return sandbox{}, fmt.Errorf("could not create the agent's temporary folder: %w", err)
	}
	paths.Temp, paths.RunTemp = realPath(sharedTemp), realPath(runTemp)
	removeTemp := func() { _ = os.RemoveAll(runTemp) }
	f, err := os.CreateTemp("", "ldcli-agent-*.sb")
	if err != nil {
		removeTemp()
		return sandbox{}, err
	}
	if _, err := f.WriteString(seatbeltProfile(paths)); err != nil {
		f.Close()
		os.Remove(f.Name())
		removeTemp()
		return sandbox{}, err
	}
	f.Close()
	return sandbox{
		Name:    sandboxExec,
		Args:    append([]string{"-f", f.Name(), binary}, args...),
		TempDir: runTemp,
		Cleanup: func() {
			_ = os.Remove(f.Name())
			removeTemp()
		},
	}, nil
}

// sharedTemp is the shared temporary folder on macOS. /tmp points to it.
const sharedTemp = "/private/tmp"

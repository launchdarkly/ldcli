// Package repoconfig persists `flags usage` settings that are inherently
// per-repository (which wrapper module/definitions dir a monorepo uses) rather
// than per-user, so they don't belong in ldcli's global ~/.config/ldcli/config.yml.
// Unlike the global config, this file lives at the scanned repo's root and is
// safe to check into version control so a team shares the same settings.
package repoconfig

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Filename is the repo-local config file, written at the repo root.
const Filename = ".ldcli-flags-usage.yml"

// Config is the subset of `flags usage` flags that make sense to pin per-repo.
type Config struct {
	WrapperModules string `yaml:"wrapper-modules,omitempty"`
	Definitions    string `yaml:"definitions,omitempty"`
}

// FindRepoRoot walks up from dir looking for a `.git` entry (directory or file,
// the latter for git worktrees) and returns its parent. ok is false if none is
// found before reaching the filesystem root.
func FindRepoRoot(dir string) (root string, ok bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Load reads the repo-local config at repoRoot. A missing file is not an
// error — it returns a zero-value Config, since most repos won't have one.
func Load(repoRoot string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, Filename))
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Save writes cfg to repoRoot, creating the file only when called — `flags
// usage` never writes it implicitly, only when the caller passes --save.
func Save(repoRoot string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(repoRoot, Filename), data, 0o644)
}

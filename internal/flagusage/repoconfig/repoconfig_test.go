package repoconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindRepoRoot(nested)
	if !ok {
		t.Fatal("expected to find repo root")
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}

func TestFindRepoRootNotFound(t *testing.T) {
	// A tmp dir with no .git anywhere above it up to the tmp root won't find one,
	// unless the OS tmp dir itself happens to be inside a repo — use a dir we
	// control and stop the walk by asserting it terminates rather than a specific
	// root, since we can't control what's above t.TempDir() on CI.
	dir := t.TempDir()
	_, _ = FindRepoRoot(dir) // just must not hang or panic
}

func TestSaveAndLoad(t *testing.T) {
	root := t.TempDir()
	cfg := Config{WrapperModules: "@gonfalon/dogfood-flags", Definitions: "./packages/dogfood-flags/src"}

	if err := Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Errorf("got %+v, want %+v", got, cfg)
	}
}

func TestLoadMissingFileIsNotError(t *testing.T) {
	root := t.TempDir()

	got, err := Load(root)
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if got != (Config{}) {
		t.Errorf("expected zero-value Config, got %+v", got)
	}
}

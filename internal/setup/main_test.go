package setup

import (
	"os"
	"testing"
)

// TestMain neutralises the ambient virtualenv for the whole package. pipInstallCmd
// prefers VIRTUAL_ENV over anything on PATH, so a developer running the suite
// inside an activated environment would otherwise see install commands resolve to
// that environment's pip and assertions about pip/pip3 fail. Tests that care about
// an active virtualenv opt in with stubVirtualEnv.
func TestMain(m *testing.M) {
	virtualEnv = func() string { return "" }
	os.Exit(m.Run())
}

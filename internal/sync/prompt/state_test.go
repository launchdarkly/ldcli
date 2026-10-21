package prompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWarnUnavailableSettingsWarnsOnceInARun(t *testing.T) {
	workspace := syncWorkspace{warnedSettings: make(map[string]bool)}
	state := workspaceState{noSettings: []string{"production"}}
	var output bytes.Buffer

	workspace.warnUnavailableSettings(&output, state)
	workspace.warnUnavailableSettings(&output, state)

	assert.Equal(t, 1, strings.Count(output.String(), "Warning: sync settings are not available for project production."))
}

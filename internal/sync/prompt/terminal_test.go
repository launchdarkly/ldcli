package prompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmApply(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		terminal  bool
		confirmed bool
		wantError string
	}{
		{name: "yes", input: "yes\n", terminal: true, confirmed: true},
		{name: "declined", input: "n\n", terminal: true},
		{name: "non-terminal", wantError: "rerun with --yes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var prompt bytes.Buffer
			confirmed, err := confirmApply(strings.NewReader(test.input), &prompt, test.terminal)

			assert.Equal(t, test.confirmed, confirmed)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
				assert.Contains(t, prompt.String(), "Sync these changes?")
			}
		})
	}
}

package validators_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/ldcli/cmd/ldcli/validators"
)

func TestCmdError(t *testing.T) {
	t.Run("with missing access-token value shows additional help", func(t *testing.T) {
		var expected string
		expected += "required flag(s) \"access-token\" not set.\n\n"
		expected += "Go to http://test.com/settings/authorization to create an access token.\n"
		expected += "Use `ldcli config --set access-token <value>` to configure the value to persist across CLI commands.\n\n"
		expected += "See `ldcli command action --help` for supported flags and usage."

		err := validators.CmdError(
			errors.New(`required flag(s) "access-token" not set`),
			"ldcli command action",
			"http://test.com",
		)

		assert.EqualError(t, err, expected)
	})

	t.Run("with missing other flag value shows regular help", func(t *testing.T) {
		expected := "required flag(s) \"my-flag\" not set. See `ldcli command action --help` for supported flags and usage."

		err := validators.CmdError(
			errors.New(`required flag(s) "my-flag" not set`),
			"ldcli command action",
			"",
		)

		assert.EqualError(t, err, expected)
	})
}

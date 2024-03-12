package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelloCmd(t *testing.T) {
	t.Run("with no options", func(t *testing.T) {
		actual := bytes.NewBufferString("")
		expected := `{"hello": "world"}`
		RootCmd.SetOut(actual)
		RootCmd.SetErr(actual)
		RootCmd.SetArgs([]string{"hello"})

		err := RootCmd.Execute()

		require.NoError(t, err)
		assert.JSONEq(t, expected, actual.String())
	})

	t.Run("with the informal option", func(t *testing.T) {
		actual := bytes.NewBufferString("")
		expected := `{"hi": "world"}`
		RootCmd.SetOut(actual)
		RootCmd.SetErr(actual)
		RootCmd.SetArgs([]string{"hello", "--informal"})

		err := RootCmd.Execute()

		require.NoError(t, err)
		assert.JSONEq(t, expected, actual.String())
	})
}

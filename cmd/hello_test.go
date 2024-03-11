package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelloCmd(t *testing.T) {
	t.Run("with no options", func(t *testing.T) {
		expected := `{"hello": "world"}`
		actual := new(bytes.Buffer)

		rootCmd.SetOut(actual)
		rootCmd.SetErr(actual)
		rootCmd.SetArgs([]string{"hello"})

		err := rootCmd.Execute()

		require.NoError(t, err)
		assert.JSONEq(t, expected, actual.String())
	})

	t.Run("with the informal option", func(t *testing.T) {
		expected := `{"hi": "world"}`
		actual := new(bytes.Buffer)
		rootCmd.SetOut(actual)
		rootCmd.SetErr(actual)
		rootCmd.SetArgs([]string{"hello", "--informal"})

		err := rootCmd.Execute()

		require.NoError(t, err)
		assert.JSONEq(t, expected, actual.String())
	})
}

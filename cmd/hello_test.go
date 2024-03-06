package cmd

import (
	"bytes"
	"testing"

	"github.com/zeebo/assert"
)

func TestHelloCmd(t *testing.T) {
	actual := new(bytes.Buffer)
	rootCmd.SetOut(actual)
	rootCmd.SetErr(actual)
	rootCmd.SetArgs([]string{"hello"})
	rootCmd.Execute()

	expected := "{\"hello\": \"world\"}"

	assert.Equal(t, actual.String(), expected)
}

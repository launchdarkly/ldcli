package output_test

import (
	"ldcli/internal/output"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmdOutput(t *testing.T) {
	t.Run("when creating a resource", func(t *testing.T) {
		input := `{
			"key": "test-key",
			"name": "test-name",
			"other": "other-value"
		}`

		t.Run("with json output", func(t *testing.T) {
			t.Run("returns the JSON", func(t *testing.T) {
				result, err := output.CmdOutput("create", "json", []byte(input))

				require.NoError(t, err)
				assert.JSONEq(t, input, result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "Successfully created test-name (test-key)"

				result, err := output.CmdOutput("create", "plaintext", []byte(input))

				require.NoError(t, err)
				assert.Equal(t, expected, result)
			})
		})
	})

	t.Run("when deleting a resource", func(t *testing.T) {
		input := `{
			"key": "test-key",
			"name": "test-name"
		}`

		t.Run("with json output", func(t *testing.T) {
			t.Run("does not return anything", func(t *testing.T) {
				result, err := output.CmdOutput("delete", "json", []byte(input))

				require.NoError(t, err)
				assert.Equal(t, "", result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("with a key and name", func(t *testing.T) {
				t.Run("returns a success message", func(t *testing.T) {
					expected := "Successfully deleted test-name (test-key)"

					result, err := output.CmdOutput("delete", "plaintext", []byte(input))

					require.NoError(t, err)
					assert.Equal(t, expected, result)
				})
			})

			t.Run("with a key", func(t *testing.T) {
				t.Skip()
				t.Run("returns a success message", func(t *testing.T) {
					expected := "Successfully deleted test-key"

					result, err := output.CmdOutput("delete", "plaintext", []byte(input))

					require.NoError(t, err)
					assert.Equal(t, expected, result)
				})
			})
		})
	})

	t.Run("when updating a resource", func(t *testing.T) {
		input := `{
			"key": "test-key",
			"name": "test-name",
			"other": "other-value"
		}`

		t.Run("with json output", func(t *testing.T) {
			t.Run("returns the JSON", func(t *testing.T) {
				result, err := output.CmdOutput("update", "json", []byte(input))

				require.NoError(t, err)
				assert.JSONEq(t, input, result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "Successfully updated test-name (test-key)"

				result, err := output.CmdOutput("update", "plaintext", []byte(input))

				require.NoError(t, err)
				assert.Equal(t, expected, result)
			})
		})
	})
}

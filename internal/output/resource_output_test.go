package output_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/errors"
	"ldcli/internal/output"
)

func TestCmdOutput(t *testing.T) {
	t.Run("with multiple resources with only an ID", func(t *testing.T) {
		input := `{
			"items": [
				{
					"_id": "test-id"
				}
			]
		}`

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "\n* test-id"

				result, err := output.CmdOutput("list", "plaintext", []byte(input))

				require.NoError(t, err)
				assert.Equal(t, expected, result)
			})
		})
	})

	t.Run("with multiple resources with an ID and name", func(t *testing.T) {
		input := `{
			"items": [
				{
					"_id": "test-id",
					"name": "test-name"
				}
			]
		}`

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "\n* test-name (test-id)"

				result, err := output.CmdOutput("list", "plaintext", []byte(input))

				require.NoError(t, err)
				assert.Equal(t, expected, result)
			})
		})
	})

	t.Run("when creating a resource", func(t *testing.T) {
		input := `{
			"key": "test-key",
			"name": "test-name",
			"other": "other-value"
		}`

		t.Run("with JSON output", func(t *testing.T) {
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

	t.Run("when creating multiple resources", func(t *testing.T) {
		input := `{
			"items": [
				{
					"key": "test-key",
					"name": "test-name",
					"other": "other-value"
				}
			]
		}`

		t.Run("with JSON output", func(t *testing.T) {
			t.Run("returns the JSON", func(t *testing.T) {
				result, err := output.CmdOutput("create", "json", []byte(input))

				require.NoError(t, err)
				assert.JSONEq(t, input, result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "Successfully created \n* test-name (test-key)"

				result, err := output.CmdOutput("create", "plaintext", []byte(input))

				require.NoError(t, err)
				assert.Equal(t, expected, result)
			})
		})
	})

	t.Run("when creating multiple with an email instead of a key", func(t *testing.T) {
		input := `{
			"items": [
				{
					"_id": "test-id",
					"email": "test-email",
					"other": "other-value"
				}
			]
		}`

		t.Run("with JSON output", func(t *testing.T) {
			t.Run("returns the JSON", func(t *testing.T) {
				result, err := output.CmdOutput("create", "json", []byte(input))

				require.NoError(t, err)
				assert.JSONEq(t, input, result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "Successfully created \n* test-email (test-id)"

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

		t.Run("with JSON output", func(t *testing.T) {
			t.Run("does not return anything", func(t *testing.T) {
				result, err := output.CmdOutput("delete", "json", []byte(""))

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
				t.Run("returns a success message", func(t *testing.T) {
					expected := "Successfully deleted test-key"
					input := `{
						"key": "test-key"
					}`

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

		t.Run("with JSON output", func(t *testing.T) {
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

func TestCmdOutputError(t *testing.T) {
	t.Run("with an API error", func(t *testing.T) {
		t.Run("with JSON output", func(t *testing.T) {
			expected := `{"code":"conflict", "message":"an error"}`
			err := errors.NewError(`{"code":"conflict", "message":"an error"}`)

			result := output.CmdOutputError("json", err)

			assert.JSONEq(t, expected, result)
		})

		t.Run("with plaintext output", func(t *testing.T) {
			expected := "invalid JSON"
			type testType any
			invalid := []byte(`{"invalid": true}`)
			err := json.Unmarshal(invalid, &[]testType{})
			require.Error(t, err)

			result := output.CmdOutputError("plaintext", err)

			assert.Equal(t, expected, result)
		})
	})
}

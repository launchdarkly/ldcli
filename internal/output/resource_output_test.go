package output_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
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
				expected := "* test-id"

				result, err := output.CmdOutput("list", "plaintext", []byte(input), nil)

				require.NoError(t, err)
				assert.Equal(t, expected, result)
			})
		})
	})

	t.Run("with paginated multiple resources", func(t *testing.T) {
		tests := map[string]struct {
			limit    int
			offset   int
			expected string
		}{
			"shows pagination": {
				limit:    5,
				expected: "* test-name (test-key)\nShowing results 1 - 5 of 100. Use --offset 5 for additional results.",
			},
			"with a paginated offset shows pagination": {
				limit:    5,
				offset:   5,
				expected: "* test-name (test-key)\nShowing results 6 - 10 of 100. Use --offset 10 for additional results.",
			},
			"with no additional pagination does not show offset help": {
				limit:    5,
				offset:   95,
				expected: "* test-name (test-key)\nShowing results 96 - 100 of 100.",
			},
		}
		for name, tt := range tests {
			tt := tt
			t.Run(name, func(t *testing.T) {
				input := fmt.Sprintf(
					`{
						"_links": {
							"self": {
								"href": "/my-resources?limit=%d&offset=%d",
								"type": "application/json"
							}
						},
						"items": [
							{
								"key": "test-key",
								"name": "test-name"
							}
						],
						"totalCount": 100
					}`,
					tt.limit,
					tt.offset,
				)

				result, err := output.CmdOutput("list", "plaintext", []byte(input), nil)

				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			})
		}
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
			t.Run("returns a list of resources", func(t *testing.T) {
				expected := "* test-name (test-id)"

				result, err := output.CmdOutput("list", "plaintext", []byte(input), nil)

				require.NoError(t, err)
				assert.Equal(t, expected, result)
			})
		})
	})

	t.Run("with a list of tags", func(t *testing.T) {
		input := `{
			"items": [
				"tag1",
				"tag2"
			],
			"_links": {
				"self": {
					"href": "/api/v2/tags",
					"type": "application/json"
				}
			},
			"totalCount": 2
		}`

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns the list", func(t *testing.T) {
				expected := "* tag1\n* tag2\nShowing results 1 - 2 of 2."

				result, err := output.CmdOutput("list", "plaintext", []byte(input), nil)

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
				result, err := output.CmdOutput("create", "json", []byte(input), nil)

				require.NoError(t, err)
				assert.JSONEq(t, input, result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "Successfully created test-name (test-key)"

				result, err := output.CmdOutput("create", "plaintext", []byte(input), nil)

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
				result, err := output.CmdOutput("create", "json", []byte(input), nil)

				require.NoError(t, err)
				assert.JSONEq(t, input, result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "Successfully created\n* test-name (test-key)"

				result, err := output.CmdOutput("create", "plaintext", []byte(input), nil)

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
				result, err := output.CmdOutput("create", "json", []byte(input), nil)

				require.NoError(t, err)
				assert.JSONEq(t, input, result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "Successfully created\n* test-email (test-id)"

				result, err := output.CmdOutput("create", "plaintext", []byte(input), nil)

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
				result, err := output.CmdOutput("delete", "json", []byte(""), nil)

				require.NoError(t, err)
				assert.Equal(t, "", result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("with a key and name", func(t *testing.T) {
				t.Run("returns a success message", func(t *testing.T) {
					expected := "Successfully deleted test-name (test-key)"

					result, err := output.CmdOutput("delete", "plaintext", []byte(input), nil)

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

					result, err := output.CmdOutput("delete", "plaintext", []byte(input), nil)

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
				result, err := output.CmdOutput("update", "json", []byte(input), nil)

				require.NoError(t, err)
				assert.JSONEq(t, input, result)
			})
		})

		t.Run("with plaintext output", func(t *testing.T) {
			t.Run("returns a success message", func(t *testing.T) {
				expected := "Successfully updated test-name (test-key)"

				result, err := output.CmdOutput("update", "plaintext", []byte(input), nil)

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

		t.Run("with plaintext output returns formatted message", func(t *testing.T) {
			err := errors.NewError(`{"code":"conflict", "message":"an error"}`)

			result := output.CmdOutputError("plaintext", err)

			assert.Equal(t, "an error (code: conflict)", result)
		})
	})

	t.Run("with a json.UnmarshalTypeError", func(t *testing.T) {
		t.Run("with plaintext output returns invalid JSON", func(t *testing.T) {
			type testType any
			invalid := []byte(`{"invalid": true}`)
			err := json.Unmarshal(invalid, &[]testType{})
			require.Error(t, err)

			result := output.CmdOutputError("plaintext", err)

			assert.Equal(t, "invalid JSON", result)
		})

		t.Run("with JSON output returns invalid JSON message", func(t *testing.T) {
			type testType any
			invalid := []byte(`{"invalid": true}`)
			err := json.Unmarshal(invalid, &[]testType{})
			require.Error(t, err)

			result := output.CmdOutputError("json", err)

			assert.JSONEq(t, `{"message":"invalid JSON"}`, result)
		})
	})

	t.Run("with a generic error", func(t *testing.T) {
		t.Run("with JSON output wraps message in JSON", func(t *testing.T) {
			err := fmt.Errorf("something went wrong")

			result := output.CmdOutputError("json", err)

			assert.JSONEq(t, `{"message":"something went wrong"}`, result)
		})

		t.Run("with plaintext output returns message", func(t *testing.T) {
			err := fmt.Errorf("something went wrong")

			result := output.CmdOutputError("plaintext", err)

			assert.Equal(t, "something went wrong", result)
		})
	})

	t.Run("with an error containing statusCode and suggestion", func(t *testing.T) {
		errJSON := `{"code":"not_found","message":"Not Found","statusCode":404,"suggestion":"Resource not found. Use ldcli flags list."}`

		t.Run("with JSON output includes all fields", func(t *testing.T) {
			err := errors.NewError(errJSON)

			result := output.CmdOutputError("json", err)

			assert.JSONEq(t, errJSON, result)
		})

		t.Run("with plaintext output includes suggestion line", func(t *testing.T) {
			err := errors.NewError(errJSON)

			result := output.CmdOutputError("plaintext", err)

			assert.Contains(t, result, "Not Found (code: not_found)")
			assert.Contains(t, result, "\nSuggestion: Resource not found. Use ldcli flags list.")
		})
	})

	t.Run("with an error with statusCode but no suggestion", func(t *testing.T) {
		errJSON := `{"code":"internal_server_error","message":"Internal Server Error","statusCode":500}`

		t.Run("with plaintext output does not include suggestion line", func(t *testing.T) {
			err := errors.NewError(errJSON)

			result := output.CmdOutputError("plaintext", err)

			assert.Equal(t, "Internal Server Error (code: internal_server_error)", result)
			assert.NotContains(t, result, "Suggestion:")
		})
	})
}

func TestCmdOutputWithFields(t *testing.T) {
	t.Run("filters singular resource to requested fields", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag","kind":"boolean","temporary":true,"extra":"noise"}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key", "name"})

		require.NoError(t, err)
		assert.JSONEq(t, `{"key":"my-flag","name":"My Flag"}`, result)
	})

	t.Run("filters collection items and preserves totalCount and _links", func(t *testing.T) {
		input := `{
			"items": [
				{"key":"flag-1","name":"Flag 1","kind":"boolean","extra":"noise"},
				{"key":"flag-2","name":"Flag 2","kind":"multivariate","extra":"more noise"}
			],
			"totalCount": 2,
			"_links": {
				"self": {"href":"/api/v2/flags/proj?limit=20&offset=0","type":"application/json"}
			}
		}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key", "name"})

		require.NoError(t, err)
		assert.JSONEq(t, `{
			"items": [
				{"key":"flag-1","name":"Flag 1"},
				{"key":"flag-2","name":"Flag 2"}
			],
			"totalCount": 2,
			"_links": {
				"self": {"href":"/api/v2/flags/proj?limit=20&offset=0","type":"application/json"}
			}
		}`, result)
	})

	t.Run("omits non-existent fields without error", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag"}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key", "doesNotExist"})

		require.NoError(t, err)
		assert.JSONEq(t, `{"key":"my-flag"}`, result)
	})

	t.Run("returns full JSON when fields is empty slice", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag","kind":"boolean"}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{})

		require.NoError(t, err)
		assert.JSONEq(t, input, result)
	})

	t.Run("returns full JSON when fields is nil", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag","kind":"boolean"}`

		result, err := output.CmdOutput("list", "json", []byte(input), nil)

		require.NoError(t, err)
		assert.JSONEq(t, input, result)
	})

	t.Run("returns original bytes when input is invalid JSON", func(t *testing.T) {
		input := `not valid json at all`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key"})

		require.NoError(t, err)
		assert.Equal(t, input, result)
	})

	t.Run("preserves nested objects intact when field is selected", func(t *testing.T) {
		input := `{
			"key":"my-flag",
			"name":"My Flag",
			"environments":{
				"production":{"on":true,"rules":[]},
				"staging":{"on":false,"rules":[]}
			},
			"variations":[{"value":true},{"value":false}]
		}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key", "environments"})

		require.NoError(t, err)
		assert.JSONEq(t, `{
			"key":"my-flag",
			"environments":{
				"production":{"on":true,"rules":[]},
				"staging":{"on":false,"rules":[]}
			}
		}`, result)
	})

	t.Run("trims whitespace from field names", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag","kind":"boolean"}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{" key ", " name"})

		require.NoError(t, err)
		assert.JSONEq(t, `{"key":"my-flag","name":"My Flag"}`, result)
	})

	t.Run("is ignored when output is plaintext", func(t *testing.T) {
		input := `{"key":"test-key","name":"test-name","extra":"extra-value"}`

		result, err := output.CmdOutput("create", "plaintext", []byte(input), []string{"key"})

		require.NoError(t, err)
		assert.Equal(t, "Successfully created test-name (test-key)", result)
	})

	t.Run("is ignored when output is plaintext for collections", func(t *testing.T) {
		input := `{"items":[{"key":"test-key","name":"test-name","extra":"extra-value"}]}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), []string{"key"})

		require.NoError(t, err)
		assert.Equal(t, "* test-name (test-key)", result)
	})

	t.Run("handles empty JSON object without panic", func(t *testing.T) {
		input := `{}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key"})

		require.NoError(t, err)
		assert.JSONEq(t, `{}`, result)
	})

	t.Run("handles empty collection with fields", func(t *testing.T) {
		input := `{"items":[],"totalCount":0}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key"})

		require.NoError(t, err)
		assert.JSONEq(t, `{"items":[],"totalCount":0}`, result)
	})

	t.Run("preserves scalar items in collection", func(t *testing.T) {
		input := `{"items":["tag1","tag2"],"totalCount":2}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key"})

		require.NoError(t, err)
		assert.JSONEq(t, `{"items":["tag1","tag2"],"totalCount":2}`, result)
	})

	t.Run("returns all fields when every field is requested", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag"}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key", "name"})

		require.NoError(t, err)
		assert.JSONEq(t, input, result)
	})

	t.Run("returns original input for top-level JSON array", func(t *testing.T) {
		input := `[{"key":"x"},{"key":"y"}]`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key"})

		require.NoError(t, err)
		assert.Equal(t, input, result)
	})

	t.Run("deduplicates repeated field names", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag","extra":"noise"}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"key", "key", "name"})

		require.NoError(t, err)
		assert.JSONEq(t, `{"key":"my-flag","name":"My Flag"}`, result)
	})

	t.Run("skips empty field strings and filters normally", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag","extra":"noise"}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"", " ", "key"})

		require.NoError(t, err)
		assert.JSONEq(t, `{"key":"my-flag"}`, result)
	})

	t.Run("returns full JSON when all field strings are empty", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag"}`

		result, err := output.CmdOutput("list", "json", []byte(input), []string{"", " "})

		require.NoError(t, err)
		assert.JSONEq(t, input, result)
	})
}

func TestCmdOutputWithResourceName(t *testing.T) {
	t.Run("flags list uses table format", func(t *testing.T) {
		input := `{
			"items": [
				{
					"key": "my-flag",
					"name": "My Flag",
					"kind": "boolean",
					"temporary": true,
					"tags": ["beta", "frontend"]
				},
				{
					"key": "dark-mode",
					"name": "Dark Mode",
					"kind": "boolean",
					"temporary": false,
					"tags": ["ui"]
				}
			]
		}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "KEY")
		assert.Contains(t, result, "NAME")
		assert.Contains(t, result, "KIND")
		assert.Contains(t, result, "TEMPORARY")
		assert.Contains(t, result, "TAGS")
		assert.Contains(t, result, "my-flag")
		assert.Contains(t, result, "My Flag")
		assert.Contains(t, result, "yes")
		assert.Contains(t, result, "dark-mode")
		assert.Contains(t, result, "no")
		assert.NotContains(t, result, "* ")
	})

	t.Run("flags singular uses key-value format", func(t *testing.T) {
		input := `{
			"key": "my-flag",
			"name": "My Flag",
			"kind": "boolean",
			"temporary": true,
			"creationDate": 1718438400000,
			"tags": ["beta"]
		}`

		result, err := output.CmdOutput("get", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "Key:")
		assert.Contains(t, result, "my-flag")
		assert.Contains(t, result, "Name:")
		assert.Contains(t, result, "My Flag")
		assert.Contains(t, result, "Kind:")
		assert.Contains(t, result, "boolean")
		assert.Contains(t, result, "Temporary:")
		assert.Contains(t, result, "yes")
	})

	t.Run("flags singular with success message", func(t *testing.T) {
		input := `{
			"key": "my-flag",
			"name": "My Flag",
			"kind": "boolean",
			"temporary": false
		}`

		result, err := output.CmdOutput("update", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "Successfully updated\n\nKey:")
		assert.Contains(t, result, "my-flag")
	})

	t.Run("unknown resource falls back to bullet format for lists", func(t *testing.T) {
		input := `{
			"items": [
				{
					"key": "test-key",
					"name": "test-name"
				}
			]
		}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "unknown-resource",
		})

		require.NoError(t, err)
		assert.Equal(t, "* test-name (test-key)", result)
	})

	t.Run("unknown resource falls back to name (key) for singular", func(t *testing.T) {
		input := `{
			"key": "test-key",
			"name": "test-name"
		}`

		result, err := output.CmdOutput("create", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "unknown-resource",
		})

		require.NoError(t, err)
		assert.Equal(t, "Successfully created test-name (test-key)", result)
	})

	t.Run("empty resource name falls back to bullet format", func(t *testing.T) {
		input := `{
			"items": [
				{
					"key": "test-key",
					"name": "test-name"
				}
			]
		}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil)

		require.NoError(t, err)
		assert.Equal(t, "* test-name (test-key)", result)
	})

	t.Run("table output with pagination", func(t *testing.T) {
		input := `{
			"_links": {
				"self": {
					"href": "/api/v2/flags/proj?limit=5&offset=0",
					"type": "application/json"
				}
			},
			"items": [
				{
					"key": "my-flag",
					"name": "My Flag",
					"kind": "boolean",
					"temporary": true,
					"tags": ["beta"]
				}
			],
			"totalCount": 100
		}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "KEY")
		assert.Contains(t, result, "my-flag")
		assert.Contains(t, result, "Showing results 1 - 5 of 100.")
		assert.Contains(t, result, "Use --offset 5 for additional results.")
	})

	t.Run("empty items with resource name returns no items found", func(t *testing.T) {
		input := `{"items": []}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.Equal(t, "No items found", result)
	})

	t.Run("JSON output is unaffected by resource name", func(t *testing.T) {
		input := `{
			"items": [
				{"key": "my-flag", "name": "My Flag"}
			]
		}`

		result, err := output.CmdOutput("list", "json", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.JSONEq(t, input, result)
	})

	t.Run("members list uses table format", func(t *testing.T) {
		input := `{
			"items": [
				{
					"email": "alice@example.com",
					"role": "admin",
					"lastName": "Smith",
					"firstName": "Alice"
				}
			]
		}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "members",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "EMAIL")
		assert.Contains(t, result, "ROLE")
		assert.Contains(t, result, "alice@example.com")
		assert.Contains(t, result, "admin")
		assert.NotContains(t, result, "* ")
	})

	t.Run("environments list uses table format", func(t *testing.T) {
		input := `{
			"items": [
				{
					"key": "production",
					"name": "Production",
					"color": "FF0000"
				}
			]
		}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "environments",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "KEY")
		assert.Contains(t, result, "NAME")
		assert.Contains(t, result, "COLOR")
		assert.Contains(t, result, "production")
		assert.Contains(t, result, "FF0000")
	})

	t.Run("projects list uses table format", func(t *testing.T) {
		input := `{
			"items": [
				{
					"key": "proj-1",
					"name": "Project One",
					"tags": ["tag1", "tag2"]
				}
			]
		}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "projects",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "KEY")
		assert.Contains(t, result, "proj-1")
		assert.Contains(t, result, "2")
	})

	t.Run("segments list uses table format", func(t *testing.T) {
		input := `{
			"items": [
				{
					"key": "beta-users",
					"name": "Beta Users",
					"creationDate": 1718438400000
				}
			]
		}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "segments",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "KEY")
		assert.Contains(t, result, "CREATED")
		assert.Contains(t, result, "beta-users")
		assert.Contains(t, result, "2024-06-15")
	})

	t.Run("create with list and resource name shows table with success message", func(t *testing.T) {
		input := `{
			"items": [
				{
					"key": "new-flag",
					"name": "New Flag",
					"kind": "boolean",
					"temporary": false,
					"tags": []
				}
			]
		}`

		result, err := output.CmdOutput("create", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "Successfully created")
		assert.Contains(t, result, "KEY")
		assert.Contains(t, result, "new-flag")
	})

	t.Run("segments singular uses key-value format", func(t *testing.T) {
		input := `{
			"key": "beta-users",
			"name": "Beta Users",
			"creationDate": 1718438400000
		}`

		result, err := output.CmdOutput("get", "plaintext", []byte(input), nil, output.CmdOutputOpts{
			ResourceName: "segments",
		})

		require.NoError(t, err)
		assert.Contains(t, result, "Key:")
		assert.Contains(t, result, "beta-users")
		assert.Contains(t, result, "Name:")
		assert.Contains(t, result, "Beta Users")
		assert.Contains(t, result, "Created:")
		assert.Contains(t, result, "2024-06-15")
	})

	t.Run("Fields from opts used when fields param is nil", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag","extra":"noise"}`

		result, err := output.CmdOutput("get", "json", []byte(input), nil, output.CmdOutputOpts{
			Fields:       []string{"key", "name"},
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.JSONEq(t, `{"key":"my-flag","name":"My Flag"}`, result)
	})

	t.Run("explicit fields param takes precedence over opts Fields", func(t *testing.T) {
		input := `{"key":"my-flag","name":"My Flag","extra":"noise"}`

		result, err := output.CmdOutput("get", "json", []byte(input), []string{"key"}, output.CmdOutputOpts{
			Fields:       []string{"key", "name"},
			ResourceName: "flags",
		})

		require.NoError(t, err)
		assert.JSONEq(t, `{"key":"my-flag"}`, result)
	})

	t.Run("empty items without resource name returns no items found", func(t *testing.T) {
		input := `{"items": []}`

		result, err := output.CmdOutput("list", "plaintext", []byte(input), nil)

		require.NoError(t, err)
		assert.Equal(t, "No items found", result)
	})
}

func TestCmdOutputErrorNotAffectedByFields(t *testing.T) {
	t.Run("JSON error response always returns full structure", func(t *testing.T) {
		errJSON := `{"code":"not_found","message":"Not Found","statusCode":404,"suggestion":"Try ldcli flags list."}`
		err := errors.NewError(errJSON)

		result := output.CmdOutputError("json", err)

		assert.JSONEq(t, errJSON, result)
	})

	t.Run("plaintext error response always returns full structure", func(t *testing.T) {
		errJSON := `{"code":"conflict","message":"an error"}`
		err := errors.NewError(errJSON)

		result := output.CmdOutputError("plaintext", err)

		assert.Equal(t, "an error (code: conflict)", result)
	})
}

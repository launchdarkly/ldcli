package environments_test

import (
	"encoding/json"
	"fmt"
	"ldcli/cmd"
	"strings"
	"testing"

	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/internal/environments"
	"ldcli/internal/errors"
)

func TestGet(t *testing.T) {
	errorHelp := ". See `ldcli environments get --help` for supported flags and usage."
	mockArgs := []interface{}{
		"testAccessToken",
		"http://test.com",
		"test-env",
		"test-proj",
	}

	t.Run("with valid environments calls API", func(t *testing.T) {
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			EnvironmentsClient: &client,
		}
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		output, err := cmd.CallCmd(t, clients, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with valid flags from environment variables calls API", func(t *testing.T) {
		teardownTest := cmd.SetupTestEnvVars(t)
		defer teardownTest(t)
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(cmd.ValidResponse), nil)
		clients := cmd.APIClients{
			EnvironmentsClient: &client,
		}
		args := []string{
			"environments", "get",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		output, err := cmd.CallCmd(t, clients, args)

		require.NoError(t, err)
		assert.JSONEq(t, `{"valid": true}`, string(output))
	})

	t.Run("with an error response is an error", func(t *testing.T) {
		client := environments.MockClient{}
		client.
			On("Get", mockArgs...).
			Return([]byte(`{}`), errors.NewError("An error"))
		clients := cmd.APIClients{
			EnvironmentsClient: &client,
		}
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "http://test.com",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		_, err := cmd.CallCmd(t, clients, args)

		require.EqualError(t, err, "An error")
	})

	t.Run("with missing required environments is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, `required flag(s) "access-token", "environment", "project" not set`+errorHelp)
	})

	t.Run("with missing short flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
			"-e",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, `flag needs an argument: 'e' in -e`)
	})

	t.Run("with missing long flag value is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
			"--environment",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, `flag needs an argument: --environment`)
	})

	t.Run("with invalid base-uri is an error", func(t *testing.T) {
		clients := cmd.APIClients{
			EnvironmentsClient: &environments.MockClient{},
		}
		args := []string{
			"environments", "get",
			"--access-token", "testAccessToken",
			"--base-uri", "invalid",
			"--environment", "test-env",
			"--project", "test-proj",
		}

		_, err := cmd.CallCmd(t, clients, args)

		assert.EqualError(t, err, "base-uri is invalid"+errorHelp)
	})
}

func TestOutputFlagGet(t *testing.T) {
	environment := &ldapi.Environment{
		Key:  "test-key",
		Name: "test-name",
	}

	t.Run("when flag is json it returns a JSON version of the environment", func(t *testing.T) {
		outputFlag := "json"
		expected := `{
			"_id": "",
			"_links": null,
			"apiKey": "",
			"color": "",
			"confirmChanges": false,
			"defaultTrackEvents": false,
			"defaultTtl": 0,
			"key": "test-key",
			"mobileKey": "",
			"name": "test-name",
			"requireComments": false,
			"secureMode": false,
			"tags": null
		}`

		output, err := CmdOutput(outputFlag, EnvironmentOutputter{
			environment: environment,
		})

		require.NoError(t, err)
		assert.JSONEq(t, expected, output)
	})

	t.Run("when flag is plaintext it outputs a plaintext version of the environment", func(t *testing.T) {
		outputFlag := "plaintext"
		expected := "test-name (test-key)"

		output, err := CmdOutput(outputFlag, EnvironmentOutputter{
			environment: environment,
		})

		require.NoError(t, err)
		assert.Equal(t, expected, output)
	})

	t.Run("when flag is not set defaults to plaintext", func(t *testing.T) {
		outputFlag := ""
		expected := "test-name (test-key)"

		output, err := CmdOutput(outputFlag, EnvironmentOutputter{
			environment: environment,
		})

		require.NoError(t, err)
		assert.Equal(t, expected, output)
	})

	t.Run("when flag is invalid", func(t *testing.T) {})
}

type Outputter interface {
	JSON() (string, error)
	String() string
}

type EnvironmentOutputter struct {
	environment *ldapi.Environment
}

func (o EnvironmentOutputter) JSON() (string, error) {
	responseJSON, err := json.Marshal(o.environment)
	if err != nil {
		return "", err
	}

	return string(responseJSON), nil
}

func (o EnvironmentOutputter) String() string {
	fnPlaintext := func(p *ldapi.Environment) string {
		return fmt.Sprintf("%s (%s)", p.Name, p.Key)
	}

	return formatColl([]*ldapi.Environment{o.environment}, fnPlaintext)
}

func CmdOutput(outputKind string, outputter Outputter) (string, error) {
	switch outputKind {
	case "json":
		return outputter.JSON()
	case "plaintext":
		return outputter.String(), nil
	default:
		return outputter.String(), nil
	}
}

func formatColl[T any](coll []T, fn func(T) string) string {
	lst := make([]string, 0, len(coll))
	for _, c := range coll {
		lst = append(lst, fn(c))
	}

	return strings.Join(lst, "\n")
}

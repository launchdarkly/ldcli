package resources_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ldcli/cmd/resources"
)

func TestGetTemplateData(t *testing.T) {
	actual, err := resources.GetTemplateData("test_data/test-openapi.json")
	assert.NoError(t, err)

	expectedFromFile, err := os.ReadFile("test_data/expected_template_data.json")
	require.NoError(t, err)

	var expected resources.TemplateData
	err = json.Unmarshal(expectedFromFile, &expected)
	require.NoError(t, err)

	t.Run("succeeds with single get resource", func(t *testing.T) {
		actualOperation := actual.Resources["Teams"].Operations["getTeam"]
		expectedOperation := expected.Resources["Teams"].Operations["getTeam"]

		assert.True(t, reflect.DeepEqual(actualOperation, expectedOperation))
	})

	t.Run("succeeds with single create resource", func(t *testing.T) {
		actualOperation := actual.Resources["Teams"].Operations["postTeam"]
		expectedOperation := expected.Resources["Teams"].Operations["postTeam"]

		assert.True(t, reflect.DeepEqual(actualOperation, expectedOperation))
	})

	t.Run("succeeds with single update resource", func(t *testing.T) {
		actualOperation := actual.Resources["Teams"].Operations["patchTeam"]
		expectedOperation := expected.Resources["Teams"].Operations["patchTeam"]

		assert.True(t, reflect.DeepEqual(actualOperation, expectedOperation))
	})

	t.Run("succeeds with get all resources", func(t *testing.T) {
		actualOperation := actual.Resources["Teams"].Operations["listTeams"]
		expectedOperation := expected.Resources["Teams"].Operations["listTeams"]

		assert.True(t, reflect.DeepEqual(actualOperation, expectedOperation))
	})
}

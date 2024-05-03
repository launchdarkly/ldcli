package resources_test

import (
	"encoding/json"
	"os"
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

	assert.Equal(t, expected, actual)
}

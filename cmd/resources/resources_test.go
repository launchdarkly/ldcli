package resources_test

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/cmd/resources"
)

func TestNewResourceData(t *testing.T) {
	t.Run("converts OpenAPI spec to a resource", func(t *testing.T) {
		tag := openapi3.Tag{
			Name:        "My resources",
			Description: "description",
		}

		resource := resources.NewResourceData(tag)

		assert.Equal(t, "MyResources", resource.GoName)
		assert.Equal(t, "my resources", resource.DisplayName)
		assert.Equal(t, `"description"`, resource.Description)
	})
}

func TestNewResources(t *testing.T) {
	t.Run("builds a map of resources", func(t *testing.T) {
		tags := openapi3.Tags{
			{
				Name: "My resources",
			},
			{
				Name: "My resources2",
			},
			{
				Name: "My resources (beta)",
			},
		}

		rs := resources.NewResources(tags)

		require.Len(t, rs, 3)
		assert.Equal(t, "MyResources", rs["my-resources"].GoName)
		assert.Equal(t, "MyResources2", rs["my-resources-2"].GoName)
		assert.Equal(t, "MyResourcesBeta", rs["my-resources-beta"].GoName)
	})

	t.Run("skips certain endpoints", func(t *testing.T) {
		tests := map[string]struct {
			name string
		}{
			"skips oauth2- endpoints": {
				name: "OAuth2 clients",
			},
			"skips other endpoints": {
				name: "Other",
			},
		}
		for name, tt := range tests {
			tt := tt
			t.Run(name, func(t *testing.T) {
				tags := openapi3.Tags{
					{
						Name: tt.name,
					},
				}

				rs := resources.NewResources(tags)

				assert.Len(t, rs, 0)
			})
		}
	})
}

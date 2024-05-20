package flags_test

import (
	"fmt"
	"github.com/launchdarkly/ldcli/internal/flags"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNameToKey(t *testing.T) {
	t.Run("with valid input", func(t *testing.T) {
		tests := map[string]struct {
			name        string
			expectedKey string
		}{
			"converts camel case to kebab case": {
				name:        "myFlag",
				expectedKey: "my-flag",
			},
			"converts multiple uppercase to kebab case": {
				name:        "myNewFlag",
				expectedKey: "my-new-flag",
			},
			"converts leading capital camel case to kebab case": {
				name:        "MyFlag",
				expectedKey: "my-flag",
			},
			"converts multiple consecutive capitals to kebab case": {
				name:        "MyFLag",
				expectedKey: "my-f-lag",
			},
			"converts space with capital to kebab case": {
				name:        "My Flag",
				expectedKey: "my-flag",
			},
			"converts multiple spaces to kebab case": {
				name:        "my   flag",
				expectedKey: "my-flag",
			},
			"converts tab to kebab case": {
				name:        "my\tflag",
				expectedKey: "my-flag",
			},
			"does not convert all lowercase": {
				name:        "myflag",
				expectedKey: "myflag",
			},
			"allows leading number": {
				name:        "1Flag",
				expectedKey: "1-flag",
			},
			"allows period": {
				name:        "my.Flag",
				expectedKey: "my.-flag",
			},
			"allows underscore": {
				name:        "my_Flag",
				expectedKey: "my_-flag",
			},
			"allows dash": {
				name:        "my-Flag",
				expectedKey: "my-flag",
			},
			"allows double dash with capital letter": {
				name:        "my--Flag",
				expectedKey: "my--flag",
			},
			"allows double dash": {
				name:        "my--flag",
				expectedKey: "my--flag",
			},
		}
		for name, tt := range tests {
			tt := tt
			t.Run(name, func(t *testing.T) {
				key, err := flags.NewKeyFromName(tt.name)

				errMsg := fmt.Sprintf("name: %s", tt.name)
				require.NoError(t, err, errMsg)
				assert.Equal(t, tt.expectedKey, key, errMsg)
			})
		}
	})

	t.Run("with invalid input", func(t *testing.T) {
		tests := map[string]struct {
			name        string
			expectedKey string
			expectedErr string
		}{
			"does not allow non-alphanumeric": {
				name:        "my-$-flag",
				expectedErr: "Name must start with a letter or number and only contain letters, numbers, '.', '_' or '-'.",
			},
			"does not allow empty name": {
				name:        "",
				expectedErr: "Name must not be empty.",
			},
			"does not allow name > 50 characters": {
				name:        strings.Repeat("*", 51),
				expectedErr: "Name must be less than 50 characters.",
			},
		}
		for name, tt := range tests {
			tt := tt
			t.Run(name, func(t *testing.T) {
				_, err := flags.NewKeyFromName(tt.name)

				assert.EqualError(t, err, tt.expectedErr)
			})
		}
	})
}

package evaluate

import (
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestMultiContextArguments(t *testing.T) {
	tests := []struct {
		name                 string
		args                 []string
		wantParseError       bool
		expectedParseError   string
		wantContextError     bool
		expectedContextError string
		expectedContext      string
	}{
		{
			name:            "basic user context",
			args:            []string{"-f=key=somekey", "-f=user=someone", "-f=country=somewhere"},
			expectedContext: `{"country":"somewhere", "key":"somekey", "kind":"user", "user":"someone"}`,
		},
		{
			name: "multi context",
			args: []string{
				"-f=key=somekey", "-f=user=someone", "-f=country=somewhere",
				"-f=key=something", "-f=kind=device",
			},
			expectedContext: `{"kind":"multi","device":{"key":"something"},"user":{"key":"somekey","country":"somewhere","user":"someone"}}`,
		},
		{
			name: "multi context flag",
			args: []string{
				"-f=key=somekey", "-f=user=someone", "-f=country=somewhere",
				"-f=key=something", "-f=kind=device",
				`--context={"kind":"plane","key":"boeing"}`,
			},
			expectedContext: `{"kind":"multi","device":{"key":"something"},"plane":{"key":"boeing"},"user":{"key":"somekey","country":"somewhere","user":"someone"}}`,
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert := assert.New(t)
			require := require.New(t)

			contextFlag := newContextFlag(os.Stdin)

			fs := pflag.NewFlagSet("testing", pflag.ContinueOnError)
			fs.Var(contextFlag.jsonValue(), "context", "Context JSON for evaluation (use @- for stdin, @filename for file)")
			fs.VarP(contextFlag.magicValue(), "field", "F", "Add field in key=value format with type conversion")
			fs.VarP(contextFlag.rawValue(), "raw-field", "f", "Add field in key=value format as raw strings")

			err := fs.Parse(tt.args)
			if tt.wantParseError {
				assert.ErrorAs(err, tt.expectedParseError)
			} else {
				require.NoError(err)
			}

			ldctx, err := contextFlag.ldContext()
			if tt.wantContextError {
				assert.ErrorAs(err, tt.expectedContextError)
			} else {
				require.NoError(err)
			}

			assert.JSONEq(tt.expectedContext, ldctx.JSONString())
		})
	}
}

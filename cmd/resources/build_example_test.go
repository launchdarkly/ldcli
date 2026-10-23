package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildExample(t *testing.T) {
	t.Run("adds required flags and one command per example", func(t *testing.T) {
		op := OperationData{
			Params: []Param{
				{Name: "project-key", In: "path"},
				{Name: "expand", In: "query"},
			},
			Examples: []RequestExample{
				{Summary: "Create a team", Data: "{\n  \"key\": \"team-key\"\n}"},
				{Data: `{"name": "Team"}`},
			},
		}

		expected := `  # Create a team
  ldcli teams create --project <project> --data '{
    "key": "team-key"
  }'

  ldcli teams create --project <project> --data '{"name": "Team"}'`

		assert.Equal(t, expected, buildExample("ldcli teams create", op))
	})

	t.Run("escapes single quotes for the shell", func(t *testing.T) {
		op := OperationData{
			Examples: []RequestExample{
				{Data: `{"comment": "don't"}`},
			},
		}

		assert.Equal(t, `  ldcli teams create --data '{"comment": "don'\''t"}'`, buildExample("ldcli teams create", op))
	})

	t.Run("returns an empty string without examples", func(t *testing.T) {
		assert.Empty(t, buildExample("ldcli teams list", OperationData{}))
	})
}

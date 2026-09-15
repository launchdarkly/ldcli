package sync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSource(t *testing.T) {
	for _, sourceType := range []SourceType{SourceTypeGit, SourceTypeLocal} {
		t.Run(string(sourceType), func(t *testing.T) {
			source, err := NewSource(sourceType, "source-identifier")

			require.NoError(t, err)
			assert.Equal(t, sourceType, source.Type())
			assert.Equal(t, "source-identifier", source.Identifier())
		})
	}
}

func TestNewSourceRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name       string
		sourceType SourceType
		identifier string
		wantErr    error
	}{
		{"invalid type", "filesystem", "local/project", ErrInvalidSourceType},
		{"missing identifier", SourceTypeGit, "", ErrInvalidSourceIdentifier},
		{"blank identifier", SourceTypeGit, " ", ErrInvalidSourceIdentifier},
		{"surrounding whitespace", SourceTypeGit, " source ", ErrInvalidSourceIdentifier},
		{
			"identifier too long",
			SourceTypeGit,
			strings.Repeat("a", MaxSourceIdentifierLength+1),
			ErrInvalidSourceIdentifier,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewSource(test.sourceType, test.identifier)
			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

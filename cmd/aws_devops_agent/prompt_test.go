package awsdevopsagent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadSecretTrimsThePastedToken(t *testing.T) {
	assert.Equal(t, "api-token", readSecret(strings.NewReader("api-token\n")))
}

func TestReadSecretReadsAnEmptyLine(t *testing.T) {
	assert.Empty(t, readSecret(strings.NewReader("\n")))
}

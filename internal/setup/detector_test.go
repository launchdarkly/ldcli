package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStubDetector_ReturnsError(t *testing.T) {
	d := StubDetector{}
	result, err := d.Detect("/tmp")
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestStubInstaller_ReturnsError(t *testing.T) {
	i := StubInstaller{}
	result, err := i.Install("/tmp", &DetectResult{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not yet implemented")
}

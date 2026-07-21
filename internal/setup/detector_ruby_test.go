package setup

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileDetector_DetectsRuby_Gemfile(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	writeDetectFile(t, dir, "app.rb", "# app\n")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "ruby-server-sdk", result.SDKID)
	assert.Equal(t, "Ruby", result.Language)
	assert.Equal(t, "gem", result.PackageManager)
	assert.Equal(t, filepath.Join(dir, "app.rb"), result.EntryPoint)
}

func TestFileDetector_DetectsRuby_Gemspec(t *testing.T) {
	dir := t.TempDir()
	writeDetectFile(t, dir, "mygem.gemspec", "Gem::Specification.new\n")

	result, err := FileDetector{}.Detect(dir)

	require.NoError(t, err)
	assert.Equal(t, "ruby-server-sdk", result.SDKID)
}

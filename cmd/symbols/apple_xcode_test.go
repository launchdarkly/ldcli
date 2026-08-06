package symbols

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The point of reading the build environment: a phase that says nothing at all
// still uploads what the build it is part of just produced.
func TestResolveAppleUploadFromXcode(t *testing.T) {
	t.Setenv(xcodeDSYMFolderEnv, "/dd/Build/Products/Release-iphoneos")

	upload := resolveAppleUpload(defaultPath)
	assert.Equal(t, "/dd/Build/Products/Release-iphoneos", upload.Path)
	assert.True(t, upload.FromXcode)
}

// A phase that names a path means it: uploading a dSYM from somewhere other than
// the build in progress has to stay possible.
func TestResolveAppleUploadPrefersAnExplicitPath(t *testing.T) {
	t.Setenv(xcodeDSYMFolderEnv, "/dd/Build/Products/Release-iphoneos")

	upload := resolveAppleUpload("./archives/MyApp.xcarchive/dSYMs")
	assert.Equal(t, "./archives/MyApp.xcarchive/dSYMs", upload.Path)
	assert.False(t, upload.FromXcode)
}

func TestResolveAppleUploadOutsideXcode(t *testing.T) {
	t.Setenv(xcodeDSYMFolderEnv, "")

	upload := resolveAppleUpload(defaultPath)
	assert.Equal(t, defaultPath, upload.Path)
	assert.False(t, upload.FromXcode)
}

// Xcode sets the folder for every configuration, including the ones whose debug
// information stays in the binary. Finding no dSYM there is the answer for a Debug
// build, and a build phase that failed the build over it would be one every project
// has to write a guard around.
func TestUploadAppleDSYMsFromXcodeWithoutADSYM(t *testing.T) {
	err := uploadAppleDSYMs("", "", appleUpload{Path: t.TempDir(), FromXcode: true}, "", false, false)
	assert.NoError(t, err)
}

// Asked for a path with no dSYM under it, though, there is nothing else this could
// have meant, so it is still reported.
func TestUploadAppleDSYMsWithoutADSYM(t *testing.T) {
	dir := t.TempDir()

	err := uploadAppleDSYMs("", "", appleUpload{Path: dir}, "", false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), dir)
}

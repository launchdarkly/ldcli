package symbols

import (
	"fmt"
	"os"
)

// Uploading from inside an Xcode build.
//
// The reliable moment to upload a dSYM is the build that produced it, which means
// a Run Script phase. Xcode runs one with the whole build's settings in its
// environment, so the phase can be told nothing and still know everything:
// DWARF_DSYM_FOLDER_PATH is where this build put its dSYMs.
//
// Reading it here is what keeps the phase to a single line. Otherwise every
// project writes the same two pieces of shell — a path that has to stay in step
// with the project's configuration, and a guard against the configurations that
// produce no dSYM — and gets to debug them inside a build phase, where the way you
// find out is a failed build.

// xcodeDSYMFolderEnv is the build setting naming the folder Xcode wrote this
// build's dSYMs to. It is set for every configuration, including the ones whose
// debug information format produces no dSYM at all.
const xcodeDSYMFolderEnv = "DWARF_DSYM_FOLDER_PATH"

// appleUpload is where an Apple upload reads its dSYMs from.
type appleUpload struct {
	Path string
	// FromXcode records that the path came from the surrounding build rather than
	// from the caller, which is what makes finding no dSYM there ordinary: it is
	// the answer for a Debug build, and not something to fail over.
	FromXcode bool
}

// resolveAppleUpload decides where to read dSYMs from. An explicit --path always
// wins — a phase that wants to upload something other than what it just built can
// still say so — and the build environment answers when there was no path to go on.
func resolveAppleUpload(path string) appleUpload {
	if path != defaultPath {
		return appleUpload{Path: path}
	}

	folder := os.Getenv(xcodeDSYMFolderEnv)
	if folder == "" {
		return appleUpload{Path: path}
	}

	fmt.Printf("Using the dSYMs this Xcode build produced, from %s\n", folder)
	return appleUpload{Path: folder, FromXcode: true}
}

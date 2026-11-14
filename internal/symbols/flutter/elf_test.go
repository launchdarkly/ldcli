package flutter

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
)

// note builds a single ELF note record: namesz descsz type, then the padded
// name and descriptor, matching what buildIDFromNotes parses.
func note(bo binary.ByteOrder, name string, ntype uint32, desc []byte) []byte {
	nameB := append([]byte(name), 0) // NUL-terminated
	var b []byte
	hdr := make([]byte, 12)
	bo.PutUint32(hdr[0:], uint32(len(nameB)))
	bo.PutUint32(hdr[4:], uint32(len(desc)))
	bo.PutUint32(hdr[8:], ntype)
	b = append(b, hdr...)
	b = append(b, nameB...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	b = append(b, desc...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func TestBuildIDFromNotes(t *testing.T) {
	bo := binary.LittleEndian
	id := []byte{0x0f, 0x8a, 0x1b, 0x2c, 0x3d, 0x4e, 0x5f, 0x60}

	// A GNU build-id note on its own.
	data := note(bo, "GNU", ntGNUBuildID, id)
	assert.Equal(t, "0f8a1b2c3d4e5f60", buildIDFromNotes(data, bo))

	// Preceded by an unrelated note (e.g. NT_GNU_ABI_TAG=1): must skip and find it.
	data = append(note(bo, "GNU", 1, []byte{0, 0, 0, 0, 1, 2, 3, 4}), note(bo, "GNU", ntGNUBuildID, id)...)
	assert.Equal(t, "0f8a1b2c3d4e5f60", buildIDFromNotes(data, bo))

	// No build-id note present.
	assert.Equal(t, "", buildIDFromNotes(note(bo, "GNU", 1, []byte{1, 2, 3, 4}), bo))

	// Truncated data must not panic and must return "".
	assert.Equal(t, "", buildIDFromNotes([]byte{1, 2, 3}, bo))

	// A skipped note whose descriptor ends at the very end of the data but whose
	// padding would run past it must not panic.
	unaligned := note(bo, "GNU", 1, []byte{1, 2, 3})
	assert.Equal(t, "", buildIDFromNotes(unaligned[:len(unaligned)-1], bo))

	// Big-endian is honored.
	be := binary.BigEndian
	assert.Equal(t, "0f8a1b2c3d4e5f60", buildIDFromNotes(note(be, "GNU", ntGNUBuildID, id), be))
}

// Dart names a compilation unit by its script URI, so only the plain and file://
// forms resolve to something readable; the rest keep the URI for the bundler to
// resolve or reject.
func TestResolveSourcePath(t *testing.T) {
	assert.Equal(t, "/abs/lib/main.dart", resolveSourcePath("file:///abs/lib/main.dart", ""))
	assert.Equal(t, "/abs/my app/main.dart", resolveSourcePath("file:///abs/my%20app/main.dart", ""))
	assert.Equal(t, "/build/dir/lib/main.dart", resolveSourcePath("../lib/main.dart", "/build/dir/tool"))
	assert.Equal(t, "/already/abs.dart", resolveSourcePath("/already/abs.dart", "/build/dir"))
	// Non-file schemes are not paths: they survive untouched.
	assert.Equal(t, "package:my_app/main.dart", resolveSourcePath("package:my_app/main.dart", "/build/dir"))
	assert.Equal(t, "dart:async", resolveSourcePath("dart:async", "/build/dir"))
	assert.Equal(t, "org-dartlang-sdk:///sdk/lib/core/list.dart", resolveSourcePath("org-dartlang-sdk:///sdk/lib/core/list.dart", "/build/dir"))
}

func TestHasURISchemeTreatsWindowsDriveAsPath(t *testing.T) {
	assert.True(t, hasURIScheme("package:my_app/main.dart"))
	assert.True(t, hasURIScheme("dart:async"))
	assert.False(t, hasURIScheme(`C:\src\main.dart`))
	assert.False(t, hasURIScheme("lib/main.dart"))
	assert.False(t, hasURIScheme("main.dart"))
}

func TestPlatformFromFilename(t *testing.T) {
	assert.Equal(t, "android-arm64", platformFromFilename("app.android-arm64.symbols"))
	assert.Equal(t, "ios-arm64", platformFromFilename("build/symbols/app.ios-arm64.symbols"))
	assert.Equal(t, "android-x64", platformFromFilename("app.android-x64.symbols"))
	// Names that don't match the app.<platform>.symbols shape yield "".
	assert.Equal(t, "", platformFromFilename("app..symbols"))
	assert.Equal(t, "", platformFromFilename("mapping.txt"))
}

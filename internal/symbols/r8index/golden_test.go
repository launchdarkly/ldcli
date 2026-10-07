package r8index

// The golden fixture: one mapping, and the exact bytes it encodes to.
//
// This package exists twice, in the CLI that writes indexes and the backend that reads
// them, and an index is only interchangeable between them if the two copies agree byte
// for byte. Two copies cannot import each other's tests, so what they share is a
// fixture: this file and the testdata beside it are identical in both repos, and either
// side drifting fails there rather than in production, where the symptom would be an
// unreadable index or a silently wrong frame.
//
// After an intentional format change, regenerate with
//
//	go test ./... -run TestGolden -update
//
// and copy testdata/ to the other repo in the same change. A format change also needs a
// new version in the artifact's file name, since readers pick an object by that name
// and an old index stays readable by the reader that was built for it.

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden index from the fixture mapping")

const (
	goldenMappingPath = "testdata/golden.mapping.txt"
	goldenIndexPath   = "testdata/golden.v1.index"
)

func goldenMapping(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(goldenMappingPath)
	require.NoError(t, err)
	return data
}

// Both encoders produce the golden bytes: the streaming one the CLI uses, and the
// whole-mapping one the tests here encode fixtures with.
func TestGoldenIndexBytes(t *testing.T) {
	mapping := goldenMapping(t)

	streamed, err := EncodeFrom(strings.NewReader(string(mapping)))
	require.NoError(t, err)

	if *updateGolden {
		require.NoError(t, os.WriteFile(goldenIndexPath, streamed, 0o644))
		t.Log("wrote", goldenIndexPath)
	}

	want, err := os.ReadFile(goldenIndexPath)
	require.NoError(t, err)
	assert.Equal(t, want, streamed,
		"the encoder no longer reproduces the golden index. If the format changed on purpose, re-run with -update, copy testdata/ to the other repo, and give the artifact a new version in its file name")

	parsed, err := Encode(Parse(mapping))
	require.NoError(t, err)
	assert.Equal(t, want, parsed, "the two encoders must agree, or which one built an index would matter")
}

// The bytes are only worth pinning if they answer correctly, and these are the answers
// both sides depend on: a line inside a range, a collapsed range, a name-only member,
// an inlined chain, a class with no members, and R8's recorded source file.
func TestGoldenIndexAnswers(t *testing.T) {
	raw, err := os.ReadFile(goldenIndexPath)
	require.NoError(t, err)
	ix, err := Open(raw)
	require.NoError(t, err)

	frames := ix.Retrace("a.b.c", "a", 2)
	require.Len(t, frames, 1)
	assert.Equal(t, "com.example.app.UserService", frames[0].Class)
	assert.Equal(t, "loadUser", frames[0].Method)
	assert.Equal(t, 41, frames[0].Line, "line 2 of range 1:3 is original 40 + 1")

	frames = ix.Retrace("a.b.c", "b", 5)
	require.Len(t, frames, 1)
	assert.Equal(t, 60, frames[0].Line, "a collapsed range answers one line whatever it is asked")

	frames = ix.Retrace("a.b.c", "d", 99)
	require.Len(t, frames, 1)
	assert.Equal(t, "helper", frames[0].Method)
	assert.Equal(t, 99, frames[0].Line, "a name-only member cannot move a line")

	frames = ix.Retrace("a.b.d", "b", 20)
	require.Len(t, frames, 2)
	assert.Equal(t, "handleClick", frames[0].Method)
	assert.Equal(t, "com.example.app.Analytics", frames[1].Class)
	assert.Equal(t, "track", frames[1].Method)
	assert.True(t, frames[1].Inlined)

	// One physical frame in a synthetic lambda, three original frames.
	frames = ix.Retrace("g5.g", "invoke", 57)
	require.Len(t, frames, 3)
	assert.Equal(t, "computeTotal", frames[0].Method)
	assert.Equal(t, 19, frames[0].Line)
	assert.Equal(t, "priceOrder", frames[1].Method)
	assert.Equal(t, "startCheckout", frames[2].Method)

	original, ok := ix.RetraceClass("g5.a")
	assert.True(t, ok)
	assert.Equal(t, "com.example.app.PaymentFailedException", original,
		"a class with no members is still a name a thrown type resolves through")

	assert.Equal(t, "UserService.java", ix.SourceFile("com.example.app.UserService"))
	assert.Equal(t, "SymbolicationDemo.kt", ix.SourceFile("com.example.app.MainActivity"))
	assert.Empty(t, ix.SourceFile("com.example.app.MainActivityKt$$ExternalSyntheticLambda10"),
		"R8's synthetic placeholder names no file")
}

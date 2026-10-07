package r8index

// An opt-in check that an index a build machine actually produced answers the way this
// reader expects, over a real release mapping rather than a fixture:
//
//	go test ./backend/stacktraces/r8index/ -run RealIndex -v \
//	  -mapping .../mapping.txt -index .../mapping.v1.index
//
// The mapping is the oracle. The golden fixture makes the same guarantee automatic, but
// only over bytes this repo encoded; this is what closes the loop on an artifact that
// crossed the boundary — written by the CLI, read here.

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRealIndexMatchesMapping(t *testing.T) {
	if *realIndexPath == "" {
		t.Skip("pass -index <index the CLI built> and -mapping <the mapping it was built from>")
	}
	indexPath := *realIndexPath
	raw, err := os.ReadFile(indexPath)
	require.NoError(t, err)
	ix, err := Open(raw)
	require.NoError(t, err)

	m := Parse(realMapping(t))
	require.NotZero(t, m.Classes())

	frames := 0
	for obf, c := range m.classes {
		gotName, gotOK := ix.RetraceClass(obf)
		wantName, wantOK := m.RetraceClass(obf)
		require.Equal(t, wantOK, gotOK, obf)
		require.Equal(t, wantName, gotName, obf)
		if wantOK {
			require.Equal(t, m.SourceFile(wantName), ix.SourceFile(wantName), wantName)
		}

		for obfMethod, members := range c.members {
			for _, member := range members {
				// The first line of each recorded range, which is where a frame that
				// arrives from a device lands.
				line := member.minStart
				require.Equal(t, m.Retrace(obf, obfMethod, line), ix.Retrace(obf, obfMethod, line),
					"%s.%s:%d", obf, obfMethod, line)
				frames++
			}
		}
	}
	t.Logf("%d classes, %d frames answered identically through %s", m.Classes(), frames, indexPath)
}

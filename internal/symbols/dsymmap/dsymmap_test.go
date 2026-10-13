package dsymmap

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLookupCorruptInlineCount ensures a map whose function claims more inline
// records than the inlines table actually holds is clamped during lookup rather
// than triggering an out-of-range slice panic in inlineAt.
func TestLookupCorruptInlineCount(t *testing.T) {
	b := &Builder{
		TextVMAddr: 0x100000000,
		CPUType:    0x0100000C,
		Funcs: []Function{{
			Start: 0x1000, End: 0x1100, Name: "outer",
			Inlines: []Inline{{Start: 0x1040, End: 0x1080, Name: "inner", CallFile: "outer.swift", CallLine: 12, Depth: 1}},
		}},
		Lines: []LineRow{
			{Addr: 0x1000, File: "outer.swift", Line: 8},
			{Addr: 0x1040, File: "inner.swift", Line: 30},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, b.Encode(&buf))
	data := buf.Bytes()

	// Corrupt func[0].inlineCount (record offset 24) so its group runs far past
	// the single real inline record.
	funcsOff := binary.LittleEndian.Uint32(data[44:])
	binary.LittleEndian.PutUint32(data[funcsOff+24:], 0xFFFFFFFF)

	m, err := Open(data)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		frames := m.Lookup(0x1050) // inside the (valid) inline range
		require.NotEmpty(t, frames)
	})
}

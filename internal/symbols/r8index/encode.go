package r8index

// Writing an index.
//
// The container is a srcbundle: a sorted, string-keyed, individually gzipped blob
// store whose reads are bounds-checked and whose misses are misses rather than
// panics. What this file adds is the per-class encoding, so that a lookup is a
// binary search plus one small inflate.
//
// Class blocks are keyed by obfuscated name. The source-file table cannot live in
// them because it is keyed by *original* name: an inlined frame reports a class that
// is not the block it was found in, so there would be no block to look in. It gets
// one entry of its own, laid out as a sorted array searched in place so that holding
// it costs bytes rather than an entry per class in the build.
//
// Blocks are written in sorted order with sorted members, so equal mappings encode
// to equal bytes — which is what lets an index be addressed by the content id of the
// mapping it was built from.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"maps"
	"slices"

	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
	e "github.com/pkg/errors"
)

const (
	// classPrefix namespaces class blocks so the source-file table can share the
	// bundle without an obfuscated name ever colliding with it.
	classPrefix = "c/"
	// sourceKey holds the original-name -> source-file table.
	sourceKey = "s"

	// srcRecSize is one source-file record: two u32 offsets into the entry.
	srcRecSize = 8
)

// EncodeFrom reads a mapping.txt and writes its index without ever holding the whole
// thing: each class is encoded and compressed as its last line is read, and then
// dropped, so what stays live is the index being built rather than every class in the
// build. On a 61 MB release mapping that is a peak of ~100 MB against the ~400 MB of
// parsing it and encoding the result, most of what is left being garbage the
// collector has not needed to take yet. A build machine should not have to find
// 400 MB to produce a 5 MB file.
//
// Reports an error for input with no classes in it, which is a file that is not a
// mapping (or a build that did not obfuscate). Uploading its index would leave the
// build looking symbolicated when nothing could be retraced.
func EncodeFrom(r io.Reader) ([]byte, error) {
	var b srcbundle.Builder
	classes := 0
	sc := newScanner(func(obf string, c *class) {
		b.Add(classPrefix+obf, encodeClass(c))
		classes++
	})

	lines := bufio.NewScanner(r)
	// A mapping line is one member, but a signature with deeply generic arguments
	// can be long, and the default 64 KB would fail the whole upload over one.
	lines.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for lines.Scan() {
		sc.line(lines.Text())
	}
	if err := lines.Err(); err != nil {
		return nil, e.Wrap(err, "r8index: reading mapping")
	}
	sc.flush()

	if classes == 0 {
		return nil, e.New("r8index: no classes in mapping")
	}
	return finishIndex(&b, sc.sourceFiles)
}

// Encode encodes an already-parsed mapping. EncodeFrom is what a producer should
// reach for; this exists for a caller that has a Mapping anyway, and for tests that
// state their input as mapping text.
func Encode(m *Mapping) ([]byte, error) {
	if m == nil {
		return nil, e.New("r8index: nil mapping")
	}

	var b srcbundle.Builder
	for _, obf := range slices.Sorted(maps.Keys(m.classes)) {
		if c := m.classes[obf]; c != nil {
			b.Add(classPrefix+obf, encodeClass(c))
		}
	}
	return finishIndex(&b, m.sourceFiles)
}

// finishIndex adds the source-file table and encodes the bundle. Shared so that an
// index is assembled in exactly one place however its classes were produced.
func finishIndex(b *srcbundle.Builder, sourceFiles map[string]string) ([]byte, error) {
	b.Add(sourceKey, encodeSourceFileTable(sourceFiles))

	var buf bytes.Buffer
	if err := b.Encode(&buf); err != nil {
		return nil, e.Wrap(err, "r8index: encoding bundle")
	}
	return buf.Bytes(), nil
}

func encodeClass(c *class) []byte {
	out := appendString(make([]byte, 0, 256), c.originalName)
	var flags byte
	if c.synthesized {
		flags |= 1
	}
	out = append(out, flags)

	out = binary.AppendUvarint(out, uint64(len(c.members)))
	for _, name := range slices.Sorted(maps.Keys(c.members)) {
		entries := c.members[name]
		out = appendString(out, name)
		out = binary.AppendUvarint(out, uint64(len(entries)))
		for _, mem := range entries {
			out = appendMember(out, mem)
		}
	}
	return out
}

// appendMember writes one member entry. Line numbers are signed varints because
// they come from Atoi over a file we did not write, and only the numbers a member
// actually carries are written at all.
func appendMember(out []byte, mem member) []byte {
	var flags byte
	if mem.hasRange {
		flags |= 1
	}
	if mem.hasOrig {
		flags |= 2
	}
	if mem.hasOrigEnd {
		flags |= 4
	}
	if mem.synthesized {
		flags |= 8
	}
	out = append(out, flags)

	if mem.hasRange {
		out = binary.AppendVarint(out, int64(mem.minStart))
		out = binary.AppendVarint(out, int64(mem.minEnd))
	}
	if mem.hasOrig {
		out = binary.AppendVarint(out, int64(mem.origStart))
	}
	if mem.hasOrigEnd {
		out = binary.AppendVarint(out, int64(mem.origEnd))
	}
	return appendString(appendString(out, mem.origClass), mem.origMethod)
}

func appendString(out []byte, s string) []byte {
	return append(binary.AppendUvarint(out, uint64(len(s))), s...)
}

// encodeSourceFileTable lays the table out as a count, a sorted array of two
// offsets per class, then the strings — so a lookup binary searches the array and
// compares against the bytes in place, without building a map of every class in
// the build to answer a handful of frames.
func encodeSourceFileTable(files map[string]string) []byte {
	names := slices.Sorted(maps.Keys(files))
	strtabStart := 4 + len(names)*srcRecSize

	var strtab []byte
	interned := make(map[string]uint32, len(names))
	put := func(s string) uint32 {
		if off, ok := interned[s]; ok {
			return off
		}
		off := uint32(strtabStart + len(strtab))
		strtab = append(append(strtab, s...), 0)
		interned[s] = off
		return off
	}

	out := make([]byte, strtabStart)
	binary.LittleEndian.PutUint32(out, uint32(len(names)))
	for i, name := range names {
		nameOff, fileOff := put(name), put(files[name])
		rec := out[4+i*srcRecSize:]
		binary.LittleEndian.PutUint32(rec[0:], nameOff)
		binary.LittleEndian.PutUint32(rec[4:], fileOff)
	}
	return append(out, strtab...)
}

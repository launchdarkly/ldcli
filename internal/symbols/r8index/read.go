package r8index

// Reading an index: a binary search for the class a frame names, one inflate of its
// block, and the same retrace the parsed form runs.

import (
	"bytes"
	"encoding/binary"
	"sort"
	"sync"

	"github.com/launchdarkly/ldcli/internal/symbols/srcbundle"
	e "github.com/pkg/errors"
)

// memoLimit caps the decoded classes an index keeps. The classes a build's traces
// hit are a small, stable set that gets in first, and the cap is what keeps an index
// that is queried for long enough from growing into the parsed mapping it exists to
// avoid.
const memoLimit = 512

// Index answers a mapping's lookups out of encoded bytes. Blocks are read through
// srcbundle.Read rather than File so the only thing held is the bounded memo below.
type Index struct {
	bundle *srcbundle.Bundle

	mu      sync.Mutex
	classes map[string]*class
}

// Open returns a view over raw, which must stay alive and unmodified for as long as
// the index is used — srcbundle.Open does not copy.
func Open(raw []byte) (*Index, error) {
	bundle, err := srcbundle.Open(raw)
	if err != nil {
		return nil, e.Wrap(err, "r8index: opening bundle")
	}
	return &Index{bundle: bundle}, nil
}

// Retrace resolves an obfuscated (class, method, line), as Mapping.Retrace does.
func (ix *Index) Retrace(obfClass, obfMethod string, line int) []Frame {
	if ix == nil {
		return nil
	}
	c := ix.class(obfClass)
	if c == nil {
		return nil
	}
	return c.retrace(obfMethod, line)
}

// RetraceClass deobfuscates a bare class name, as Mapping.RetraceClass does.
func (ix *Index) RetraceClass(obfClass string) (string, bool) {
	if ix == nil {
		return "", false
	}
	c := ix.class(obfClass)
	if c == nil || c.originalName == "" {
		return "", false
	}
	return c.originalName, true
}

// SourceFile reports the file an *original* class name was compiled from. This is
// the one entry worth memoizing (via File rather than Read): there is exactly one of
// it, every frame needs it, and it is searched where it lies rather than decoded
// into anything.
func (ix *Index) SourceFile(originalClass string) string {
	if ix == nil {
		return ""
	}
	table, ok := ix.bundle.File(sourceKey)
	if !ok || len(table) < 4 {
		return ""
	}
	count := int(binary.LittleEndian.Uint32(table))
	if count < 0 || 4+count*srcRecSize > len(table) {
		return ""
	}

	nameOff := func(i int) uint32 {
		return binary.LittleEndian.Uint32(table[4+i*srcRecSize:])
	}
	i := sort.Search(count, func(i int) bool {
		return compareString(table, nameOff(i), originalClass) >= 0
	})
	if i >= count || compareString(table, nameOff(i), originalClass) != 0 {
		return ""
	}
	return tableString(table, binary.LittleEndian.Uint32(table[4+i*srcRecSize+4:]))
}

// class decodes one class block. A missing or unreadable block is remembered as a
// miss, so a frame from a stale mapping costs one lookup rather than one per frame.
func (ix *Index) class(obfClass string) *class {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	if c, ok := ix.classes[obfClass]; ok {
		return c
	}
	var c *class
	if block, ok := ix.bundle.Read(classPrefix + obfClass); ok {
		if decoded, ok := decodeClass(block); ok {
			c = decoded
		}
	}
	if ix.classes == nil {
		ix.classes = make(map[string]*class)
	}
	if len(ix.classes) < memoLimit {
		ix.classes[obfClass] = c
	}
	return c
}

func decodeClass(data []byte) (*class, bool) {
	cur := &cursor{buf: data}
	c := &class{originalName: cur.string()}
	c.synthesized = cur.byte()&1 != 0

	// A count is bounded by what is left to read, so a corrupt one cannot ask for
	// an allocation the block could not possibly describe.
	methods, ok := cur.count()
	if !ok {
		return nil, false
	}
	c.members = make(map[string][]member, methods)
	for range methods {
		name := cur.string()
		entries, ok := cur.count()
		if !ok {
			return nil, false
		}
		members := make([]member, 0, entries)
		for range entries {
			members = append(members, decodeMember(cur))
		}
		if cur.err {
			return nil, false
		}
		c.members[name] = members
	}
	if cur.err {
		return nil, false
	}
	return c, true
}

func decodeMember(cur *cursor) member {
	flags := cur.byte()
	var mem member
	mem.hasRange = flags&1 != 0
	mem.hasOrig = flags&2 != 0
	mem.hasOrigEnd = flags&4 != 0
	mem.synthesized = flags&8 != 0

	if mem.hasRange {
		mem.minStart = int(cur.varint())
		mem.minEnd = int(cur.varint())
	}
	if mem.hasOrig {
		mem.origStart = int(cur.varint())
	}
	if mem.hasOrigEnd {
		mem.origEnd = int(cur.varint())
	}
	mem.origClass = cur.string()
	mem.origMethod = cur.string()
	return mem
}

// cursor reads a block, latching the first read that ran past the end so callers can
// decode straight through and check once.
type cursor struct {
	buf []byte
	err bool
}

func (c *cursor) byte() byte {
	if len(c.buf) < 1 {
		c.err = true
		return 0
	}
	b := c.buf[0]
	c.buf = c.buf[1:]
	return b
}

func (c *cursor) uvarint() uint64 {
	v, n := binary.Uvarint(c.buf)
	if n <= 0 {
		c.err = true
		return 0
	}
	c.buf = c.buf[n:]
	return v
}

func (c *cursor) varint() int64 {
	v, n := binary.Varint(c.buf)
	if n <= 0 {
		c.err = true
		return 0
	}
	c.buf = c.buf[n:]
	return v
}

func (c *cursor) string() string {
	n := c.uvarint()
	if c.err || n > uint64(len(c.buf)) {
		c.err = true
		return ""
	}
	s := string(c.buf[:n])
	c.buf = c.buf[n:]
	return s
}

// count reads a length that is about to size an allocation, rejecting one larger
// than the bytes left could describe.
func (c *cursor) count() (int, bool) {
	n := c.uvarint()
	if c.err || n > uint64(len(c.buf)) {
		c.err = true
		return 0, false
	}
	return int(n), true
}

// compareString compares the NUL-terminated string at off against s without
// building a string to compare with.
func compareString(table []byte, off uint32, s string) int {
	i := int(off)
	if i >= len(table) {
		return -1
	}
	for k := 0; k < len(s); k++ {
		if i >= len(table) || table[i] == 0 {
			return -1
		}
		if table[i] != s[k] {
			if table[i] < s[k] {
				return -1
			}
			return 1
		}
		i++
	}
	if i < len(table) && table[i] != 0 {
		return 1
	}
	return 0
}

func tableString(table []byte, off uint32) string {
	if int(off) >= len(table) {
		return ""
	}
	s := table[off:]
	if i := bytes.IndexByte(s, 0); i >= 0 {
		return string(s[:i])
	}
	return string(s)
}

// Package srcbundle implements the Source Bundle (".srcbundle") format: a
// compact, binary-searchable archive of the source files referenced by a dSYM's
// DWARF, used to render source context (the lines around a frame) for native
// stack frames.
//
// It is produced by `ldcli symbols upload --type apple-dsym --include-sources`
// and consumed by the backend Apple enhancer, which hydrates
// lineContent/linesBefore/linesAfter after a frame resolves to file:line. The two
// sides live in separate repos and intentionally duplicate this layout rather
// than share a module; this file is byte-for-byte identical to
// backend/stacktraces/srcbundle/srcbundle.go, and the `Magic`+`Version` header
// guards against drift.
//
// # Keying
//
// Files are keyed by the *exact* path string the sibling .dsymmap stores for the
// same build (DWARF's file name). Both artifacts are produced from one DWARF
// pass, so a frame resolved through the map looks its source up here with no
// path normalization.
//
// # Design goals
//
//   - Fetch little: each file is compressed independently, so answering one frame
//     decompresses exactly one file, not the archive.
//   - O(log n) lookup: the index is a sorted, fixed-width array probed by binary
//     search over the string table.
//   - Bounded: the reader validates every offset/length against the buffer, so a
//     truncated or hostile bundle yields misses instead of panics.
//
// # Binary layout (little-endian)
//
//	Header (32 bytes):
//	  0  magic      [4]byte "SRCB"
//	  4  version    u16
//	  6  flags      u16   (bit 0: payload entries are gzip-compressed)
//	  8  nFiles     u32
//	  12 indexOff   u32
//	  16 strtabOff  u32
//	  20 strtabLen  u32
//	  24 payloadOff u32
//	  28 payloadLen u32
//
//	index[] (nFiles × 16 bytes, sorted by path string, ascending):
//	  0  pathOff    u32   (into strtab)
//	  4  dataOff    u32   (into payload)
//	  8  compLen    u32   (stored bytes)
//	  12 rawLen     u32   (decompressed bytes)
//
//	strtab: NUL-terminated UTF-8 paths; offset 0 is the empty string.
//	payload: per-file gzip streams, concatenated.
package srcbundle

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"sort"
	"sync"

	e "github.com/pkg/errors"
)

const (
	// Magic identifies the file format.
	Magic = "SRCB"
	// Version is bumped on any incompatible layout change; the reader rejects
	// versions it does not understand.
	Version = uint16(1)

	headerSize   = 32
	indexRecSize = 16

	// flagGzip marks payload entries as individually gzip-compressed.
	flagGzip = uint16(1)
)

var le = binary.LittleEndian

// --- Builder (encode side) ---

// Builder accumulates source files for one image and encodes them. Add is
// keyed by the same path string the sibling .dsymmap records.
type Builder struct {
	files map[string][]byte
}

// Add registers one source file's contents under path. Empty paths and repeats
// are ignored, so callers can add freely while walking DWARF.
func (b *Builder) Add(path string, content []byte) {
	if path == "" {
		return
	}
	if b.files == nil {
		b.files = make(map[string][]byte)
	}
	if _, ok := b.files[path]; ok {
		return
	}
	b.files[path] = content
}

// Len is the number of distinct files added.
func (b *Builder) Len() int { return len(b.files) }

// Encode writes the bundle. Paths are sorted so the reader can binary search.
func (b *Builder) Encode(w io.Writer) error {
	paths := make([]string, 0, len(b.files))
	for p := range b.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var strtab bytes.Buffer
	strtab.WriteByte(0) // offset 0 == ""
	var payload bytes.Buffer
	index := make([]byte, 0, len(paths)*indexRecSize)

	for _, p := range paths {
		pathOff := uint32(strtab.Len())
		strtab.WriteString(p)
		strtab.WriteByte(0)

		raw := b.files[p]
		dataOff := uint32(payload.Len())
		zw := gzip.NewWriter(&payload)
		if _, err := zw.Write(raw); err != nil {
			return e.Wrapf(err, "srcbundle: compressing %s", p)
		}
		if err := zw.Close(); err != nil {
			return e.Wrapf(err, "srcbundle: finishing %s", p)
		}

		rec := make([]byte, indexRecSize)
		le.PutUint32(rec[0:], pathOff)
		le.PutUint32(rec[4:], dataOff)
		le.PutUint32(rec[8:], uint32(payload.Len())-dataOff)
		le.PutUint32(rec[12:], uint32(len(raw)))
		index = append(index, rec...)
	}

	indexOff := uint32(headerSize)
	strtabOff := indexOff + uint32(len(index))
	payloadOff := strtabOff + uint32(strtab.Len())

	hdr := make([]byte, headerSize)
	copy(hdr[0:4], Magic)
	le.PutUint16(hdr[4:], Version)
	le.PutUint16(hdr[6:], flagGzip)
	le.PutUint32(hdr[8:], uint32(len(paths)))
	le.PutUint32(hdr[12:], indexOff)
	le.PutUint32(hdr[16:], strtabOff)
	le.PutUint32(hdr[20:], uint32(strtab.Len()))
	le.PutUint32(hdr[24:], payloadOff)
	le.PutUint32(hdr[28:], uint32(payload.Len()))

	for _, chunk := range [][]byte{hdr, index, strtab.Bytes(), payload.Bytes()} {
		if _, err := w.Write(chunk); err != nil {
			return e.Wrap(err, "srcbundle: writing")
		}
	}
	return nil
}

// --- Bundle (decode side) ---

// Bundle is a read-only view over encoded bundle bytes. Decompressed files are
// memoized, so several frames in one file cost a single inflate.
type Bundle struct {
	nFiles  int
	index   []byte
	strtab  []byte
	payload []byte
	gzipped bool

	mu   sync.Mutex
	memo map[string][]byte
}

// Open validates the header and returns a view over raw. It does not copy: raw
// must stay alive (and unmodified) for the lifetime of the Bundle.
func Open(raw []byte) (*Bundle, error) {
	if len(raw) < headerSize {
		return nil, e.New("srcbundle: buffer shorter than header")
	}
	if string(raw[0:4]) != Magic {
		return nil, e.New("srcbundle: bad magic")
	}
	if v := le.Uint16(raw[4:]); v != Version {
		return nil, e.Errorf("srcbundle: unsupported version %d", v)
	}

	flags := le.Uint16(raw[6:])
	nFiles := le.Uint32(raw[8:])
	// Every section is bounds-checked against the buffer up front so lookups can
	// slice without re-validating (and a truncated bundle fails loudly here
	// rather than panicking mid-query).
	slice := func(off, length uint32) ([]byte, bool) {
		hi := uint64(off) + uint64(length)
		if hi > uint64(len(raw)) {
			return nil, false
		}
		return raw[off:hi], true
	}

	indexLen := uint64(nFiles) * indexRecSize
	if indexLen > uint64(len(raw)) {
		return nil, e.New("srcbundle: index length out of range")
	}
	index, ok := slice(le.Uint32(raw[12:]), uint32(indexLen))
	if !ok {
		return nil, e.New("srcbundle: index out of range")
	}
	strtab, ok := slice(le.Uint32(raw[16:]), le.Uint32(raw[20:]))
	if !ok {
		return nil, e.New("srcbundle: strtab out of range")
	}
	payload, ok := slice(le.Uint32(raw[24:]), le.Uint32(raw[28:]))
	if !ok {
		return nil, e.New("srcbundle: payload out of range")
	}

	return &Bundle{
		nFiles:  int(nFiles),
		index:   index,
		strtab:  strtab,
		payload: payload,
		gzipped: flags&flagGzip != 0,
	}, nil
}

// Len is the number of files in the bundle.
func (b *Bundle) Len() int { return b.nFiles }

func (b *Bundle) str(off uint32) string {
	if int(off) >= len(b.strtab) {
		return ""
	}
	s := b.strtab[off:]
	if i := bytes.IndexByte(s, 0); i >= 0 {
		return string(s[:i])
	}
	return string(s)
}

func (b *Bundle) rec(i int) (pathOff, dataOff, compLen, rawLen uint32) {
	r := b.index[i*indexRecSize:]
	return le.Uint32(r[0:]), le.Uint32(r[4:]), le.Uint32(r[8:]), le.Uint32(r[12:])
}

// find binary searches the sorted index for an exact path match.
func (b *Bundle) find(path string) (int, bool) {
	i := sort.Search(b.nFiles, func(i int) bool {
		pathOff, _, _, _ := b.rec(i)
		return b.str(pathOff) >= path
	})
	if i < b.nFiles {
		pathOff, _, _, _ := b.rec(i)
		if b.str(pathOff) == path {
			return i, true
		}
	}
	return 0, false
}

// File returns the decompressed contents of one source file. Misses (including
// a corrupt entry) are memoized too, so a bad path is looked up once.
func (b *Bundle) File(path string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if v, ok := b.memo[path]; ok {
		return v, v != nil
	}

	out, ok := b.readFile(path)
	if b.memo == nil {
		b.memo = make(map[string][]byte)
	}
	if !ok {
		b.memo[path] = nil
		return nil, false
	}
	b.memo[path] = out
	return out, true
}

func (b *Bundle) readFile(path string) ([]byte, bool) {
	i, ok := b.find(path)
	if !ok {
		return nil, false
	}
	_, dataOff, compLen, rawLen := b.rec(i)
	hi := uint64(dataOff) + uint64(compLen)
	if hi > uint64(len(b.payload)) {
		return nil, false
	}
	blob := b.payload[dataOff:hi]
	if !b.gzipped {
		return blob, true
	}

	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, false
	}
	defer zr.Close()
	// rawLen comes from the file, so cap the inflate to it: a corrupt/hostile
	// entry can't expand without bound.
	out, err := io.ReadAll(io.LimitReader(zr, int64(rawLen)))
	if err != nil {
		return nil, false
	}
	return out, true
}

// Window returns the source context around a 1-based line: up to ctx lines
// before, the line itself, and up to ctx lines after. Every returned line keeps
// its trailing newline, matching how the JavaScript source-map path fills
// linesBefore/lineContent/linesAfter. ok is false when the file isn't bundled or
// the line is out of range.
func (b *Bundle) Window(path string, line, ctx int) (before, content, after string, ok bool) {
	if line <= 0 || ctx < 0 {
		return "", "", "", false
	}
	data, found := b.File(path)
	if !found {
		return "", "", "", false
	}

	starts := lineStarts(data)
	idx := line - 1
	if idx >= len(starts) {
		return "", "", "", false
	}
	lo := idx - ctx
	if lo < 0 {
		lo = 0
	}
	hi := idx + 1 + ctx
	if hi > len(starts) {
		hi = len(starts)
	}
	// Lines are contiguous in data, so a window is just two offsets.
	at := func(i int) int {
		if i >= len(starts) {
			return len(data)
		}
		return starts[i]
	}
	return string(data[at(lo):at(idx)]), string(data[at(idx):at(idx+1)]), string(data[at(idx+1):at(hi)]), true
}

// lineStarts returns the byte offset of each line. A trailing newline does not
// begin a new line, so counts match what an editor shows.
func lineStarts(data []byte) []int {
	starts := make([]int, 1, 1+bytes.Count(data, []byte{'\n'}))
	for i, c := range data {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	if n := len(starts); n > 1 && starts[n-1] == len(data) {
		starts = starts[:n-1]
	}
	return starts
}

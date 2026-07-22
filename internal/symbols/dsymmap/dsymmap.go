// Package dsymmap implements the dSYM Map (".dsymmap") format: a compact,
// mmap-friendly, binary-searchable index that maps an image-relative instruction
// offset to a function name, source file:line, and any inlined call frames.
//
// It is produced by `ldcli symbols upload --type apple-dsym` from a Mach-O dSYM
// (one file per architecture, keyed by the build UUID) and consumed by the
// backend Apple enhancer. The two sides live in separate repos and intentionally
// duplicate this layout rather than share a module; this file is byte-for-byte
// identical to backend/stacktraces/dsymmap/dsymmap.go, and the `Magic`+`Version`
// header plus a shared golden file guard against drift.
//
// # Design goals
//
//   - Zero-parse load: the reader operates directly over the raw bytes (which may
//     be an mmap'd region); no unmarshal pass and no per-record allocation.
//   - O(log n) lookup: the func/line tables are sorted, fixed-width arrays probed
//     by binary search (address ranges are predecessor queries, not exact keys).
//   - Small + off-heap: strings are deduplicated in a single table and referenced
//     by u32 offsets.
//
// # Address space
//
// All addresses are "image-relative": file_addr - text_vmaddr, where file_addr is
// the address as it appears in DWARF and text_vmaddr is the __TEXT segment's
// vmaddr. At runtime the device reports rel_offset = instruction_addr -
// image_load_addr, which is algebraically identical, so the reader can use the
// device's rel_offset directly as the lookup key with no slide math.
//
// # Binary layout (little-endian)
//
//	Header (72 bytes):
//	  0  magic       [4]byte "DSMP"
//	  4  version     u16
//	  6  flags       u16
//	  8  cpuType     u32     (mach cputype)
//	  12 cpuSubtype  u32
//	  16 uuid        [16]byte
//	  32 textVMAddr  u64     (normalization reference; not needed at query time)
//	  40 nFuncs      u32
//	  44 funcsOff    u32
//	  48 nLines      u32
//	  52 linesOff    u32
//	  56 nInlines    u32
//	  60 inlinesOff  u32
//	  64 strtabOff   u32
//	  68 strtabLen   u32
//
//	funcs[]   (nFuncs × 32 bytes, sorted by addrStart, non-overlapping):
//	  0  addrStart   u64
//	  8  addrEnd     u64
//	  16 nameOff     u32   (into strtab: demangled function name)
//	  20 inlineOff   u32   (index into inlines[]; start of this func's group)
//	  24 inlineCount u32   (number of inline records in this func's group)
//	  28 _pad        u32
//
//	lines[]   (nLines × 16 bytes, sorted by addr):
//	  0  addr        u64   (row covers [addr, next-row.addr))
//	  8  fileOff     u32   (into strtab: source file path; 0 == none)
//	  12 line        u32
//
//	inlines[] (nInlines × 32 bytes, grouped per func, sorted by depth ascending):
//	  0  addrStart   u64
//	  8  addrEnd     u64
//	  16 nameOff     u32   (into strtab: inlined function name)
//	  20 callFileOff u32   (into strtab: call-site file in the caller)
//	  24 callLine    u32   (call-site line in the caller)
//	  28 depth       u32   (1 = outermost inline; increases inward)
//
//	strtab: NUL-terminated UTF-8 strings; offset 0 is the empty string.
package dsymmap

import (
	"encoding/binary"
	"io"
	"sort"

	e "github.com/pkg/errors"
)

const (
	// Magic identifies the file format.
	Magic = "DSMP"
	// Version is bumped on any incompatible layout change; the reader rejects
	// versions it does not understand.
	Version = uint16(1)

	headerSize    = 72
	funcRecSize   = 32
	lineRecSize   = 16
	inlineRecSize = 32
)

// Frame is one resolved stack frame. Lookup returns frames innermost-first
// (deepest inline first, physical function last), matching a deepest-frame-first
// stack ordering.
type Frame struct {
	Function string
	File     string
	Line     uint32
}

// --- Builder (encode side) ---

// Inline describes one inlined call recovered from DWARF. Name is the inlined
// function; CallFile/CallLine are where it was called from (in its caller).
// Depth is 1 for the outermost inline and increases inward.
type Inline struct {
	Start, End uint64
	Name       string
	CallFile   string
	CallLine   uint32
	Depth      uint32
}

// Function is one physical function covering [Start, End). Inlines, if any, are
// the inlined calls contained within it.
type Function struct {
	Start, End uint64
	Name       string
	Inlines    []Inline
}

// LineRow maps an address to a source file:line; it is valid until the next row.
type LineRow struct {
	Addr uint64
	File string
	Line uint32
}

// Builder accumulates the tables for one architecture/UUID and encodes them.
type Builder struct {
	UUID       [16]byte
	TextVMAddr uint64
	CPUType    uint32
	CPUSubtype uint32
	Funcs      []Function
	Lines      []LineRow
}

type strtab struct {
	buf     []byte
	offsets map[string]uint32
}

func newStrtab() *strtab {
	// Offset 0 is reserved for the empty string.
	return &strtab{buf: []byte{0}, offsets: map[string]uint32{"": 0}}
}

func (s *strtab) intern(str string) uint32 {
	if off, ok := s.offsets[str]; ok {
		return off
	}
	off := uint32(len(s.buf))
	s.buf = append(s.buf, str...)
	s.buf = append(s.buf, 0)
	s.offsets[str] = off
	return off
}

// Encode writes the .dsymmap representation to w.
func (b *Builder) Encode(w io.Writer) error {
	funcs := append([]Function(nil), b.Funcs...)
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].Start < funcs[j].Start })
	lines := append([]LineRow(nil), b.Lines...)
	sort.Slice(lines, func(i, j int) bool { return lines[i].Addr < lines[j].Addr })

	st := newStrtab()

	funcBuf := make([]byte, 0, len(funcs)*funcRecSize)
	var inlineBuf []byte
	var inlineCount uint32
	for _, fn := range funcs {
		ins := append([]Inline(nil), fn.Inlines...)
		sort.Slice(ins, func(i, j int) bool { return ins[i].Depth < ins[j].Depth })
		inlineOff := inlineCount
		for _, in := range ins {
			rec := make([]byte, inlineRecSize)
			binary.LittleEndian.PutUint64(rec[0:], in.Start)
			binary.LittleEndian.PutUint64(rec[8:], in.End)
			binary.LittleEndian.PutUint32(rec[16:], st.intern(in.Name))
			binary.LittleEndian.PutUint32(rec[20:], st.intern(in.CallFile))
			binary.LittleEndian.PutUint32(rec[24:], in.CallLine)
			binary.LittleEndian.PutUint32(rec[28:], in.Depth)
			inlineBuf = append(inlineBuf, rec...)
			inlineCount++
		}

		rec := make([]byte, funcRecSize)
		binary.LittleEndian.PutUint64(rec[0:], fn.Start)
		binary.LittleEndian.PutUint64(rec[8:], fn.End)
		binary.LittleEndian.PutUint32(rec[16:], st.intern(fn.Name))
		binary.LittleEndian.PutUint32(rec[20:], inlineOff)
		binary.LittleEndian.PutUint32(rec[24:], uint32(len(ins)))
		funcBuf = append(funcBuf, rec...)
	}

	lineBuf := make([]byte, 0, len(lines)*lineRecSize)
	for _, ln := range lines {
		rec := make([]byte, lineRecSize)
		binary.LittleEndian.PutUint64(rec[0:], ln.Addr)
		binary.LittleEndian.PutUint32(rec[8:], st.intern(ln.File))
		binary.LittleEndian.PutUint32(rec[12:], ln.Line)
		lineBuf = append(lineBuf, rec...)
	}

	funcsOff := uint32(headerSize)
	linesOff := funcsOff + uint32(len(funcBuf))
	inlinesOff := linesOff + uint32(len(lineBuf))
	strtabOff := inlinesOff + uint32(len(inlineBuf))

	hdr := make([]byte, headerSize)
	copy(hdr[0:4], Magic)
	binary.LittleEndian.PutUint16(hdr[4:], Version)
	binary.LittleEndian.PutUint16(hdr[6:], 0)
	binary.LittleEndian.PutUint32(hdr[8:], b.CPUType)
	binary.LittleEndian.PutUint32(hdr[12:], b.CPUSubtype)
	copy(hdr[16:32], b.UUID[:])
	binary.LittleEndian.PutUint64(hdr[32:], b.TextVMAddr)
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(funcs)))
	binary.LittleEndian.PutUint32(hdr[44:], funcsOff)
	binary.LittleEndian.PutUint32(hdr[48:], uint32(len(lines)))
	binary.LittleEndian.PutUint32(hdr[52:], linesOff)
	binary.LittleEndian.PutUint32(hdr[56:], inlineCount)
	binary.LittleEndian.PutUint32(hdr[60:], inlinesOff)
	binary.LittleEndian.PutUint32(hdr[64:], strtabOff)
	binary.LittleEndian.PutUint32(hdr[68:], uint32(len(st.buf)))

	for _, chunk := range [][]byte{hdr, funcBuf, lineBuf, inlineBuf, st.buf} {
		if _, err := w.Write(chunk); err != nil {
			return e.Wrap(err, "writing dsymmap")
		}
	}
	return nil
}

// --- Map (decode + lookup side) ---

// Map is a read-only view over encoded .dsymmap bytes (typically an mmap'd region).
// It does not copy or parse the record sections; lookups index into data.
type Map struct {
	data       []byte
	uuid       [16]byte
	textVMAddr uint64
	cpuType    uint32
	cpuSubtype uint32

	funcs   []byte
	nFuncs  int
	lines   []byte
	nLines  int
	inlines []byte

	strtab []byte
}

// Open validates the header and returns a Map over data. data must remain valid
// (not unmapped) for the lifetime of the Map.
func Open(data []byte) (*Map, error) {
	if len(data) < headerSize {
		return nil, e.New("dsymmap: data shorter than header")
	}
	if string(data[0:4]) != Magic {
		return nil, e.Errorf("dsymmap: bad magic %q", data[0:4])
	}
	if v := binary.LittleEndian.Uint16(data[4:]); v != Version {
		return nil, e.Errorf("dsymmap: unsupported version %d (want %d)", v, Version)
	}

	m := &Map{data: data}
	m.cpuType = binary.LittleEndian.Uint32(data[8:])
	m.cpuSubtype = binary.LittleEndian.Uint32(data[12:])
	copy(m.uuid[:], data[16:32])
	m.textVMAddr = binary.LittleEndian.Uint64(data[32:])

	nFuncs := binary.LittleEndian.Uint32(data[40:])
	funcsOff := binary.LittleEndian.Uint32(data[44:])
	nLines := binary.LittleEndian.Uint32(data[48:])
	linesOff := binary.LittleEndian.Uint32(data[52:])
	nInlines := binary.LittleEndian.Uint32(data[56:])
	inlinesOff := binary.LittleEndian.Uint32(data[60:])
	strtabOff := binary.LittleEndian.Uint32(data[64:])
	strtabLen := binary.LittleEndian.Uint32(data[68:])

	var err error
	if m.funcs, err = section(data, funcsOff, nFuncs, funcRecSize, "funcs"); err != nil {
		return nil, err
	}
	if m.lines, err = section(data, linesOff, nLines, lineRecSize, "lines"); err != nil {
		return nil, err
	}
	if m.inlines, err = section(data, inlinesOff, nInlines, inlineRecSize, "inlines"); err != nil {
		return nil, err
	}
	if m.strtab, err = section(data, strtabOff, strtabLen, 1, "strtab"); err != nil {
		return nil, err
	}
	if strtabLen == 0 || m.strtab[len(m.strtab)-1] != 0 {
		return nil, e.New("dsymmap: strtab not NUL-terminated")
	}
	m.nFuncs = int(nFuncs)
	m.nLines = int(nLines)
	return m, nil
}

func section(data []byte, off, count, size uint32, name string) ([]byte, error) {
	end := uint64(off) + uint64(count)*uint64(size)
	if uint64(off) > uint64(len(data)) || end > uint64(len(data)) {
		return nil, e.Errorf("dsymmap: %s section out of bounds", name)
	}
	return data[off:end], nil
}

// UUID returns the build UUID this map symbolicates.
func (m *Map) UUID() [16]byte { return m.uuid }

// CPU returns the mach cputype/cpusubtype this map was built for.
func (m *Map) CPU() (uint32, uint32) { return m.cpuType, m.cpuSubtype }

func (m *Map) str(off uint32) string {
	if int(off) >= len(m.strtab) {
		return ""
	}
	b := m.strtab[off:]
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func (m *Map) funcAt(i int) (start, end uint64, nameOff, inlineOff, inlineCount uint32) {
	r := m.funcs[i*funcRecSize:]
	return binary.LittleEndian.Uint64(r[0:]),
		binary.LittleEndian.Uint64(r[8:]),
		binary.LittleEndian.Uint32(r[16:]),
		binary.LittleEndian.Uint32(r[20:]),
		binary.LittleEndian.Uint32(r[24:])
}

func (m *Map) lineAt(i int) (addr uint64, fileOff, line uint32) {
	r := m.lines[i*lineRecSize:]
	return binary.LittleEndian.Uint64(r[0:]),
		binary.LittleEndian.Uint32(r[8:]),
		binary.LittleEndian.Uint32(r[12:])
}

func (m *Map) inlineAt(i int) (start, end uint64, nameOff, callFileOff, callLine, depth uint32) {
	r := m.inlines[i*inlineRecSize:]
	return binary.LittleEndian.Uint64(r[0:]),
		binary.LittleEndian.Uint64(r[8:]),
		binary.LittleEndian.Uint32(r[16:]),
		binary.LittleEndian.Uint32(r[20:]),
		binary.LittleEndian.Uint32(r[24:]),
		binary.LittleEndian.Uint32(r[28:])
}

// Lookup resolves an image-relative offset to a stack of frames, innermost
// (deepest inline) first and the physical function last. It returns nil when no
// function covers the offset (the caller then falls back to the on-device
// symbol or module+offset).
func (m *Map) Lookup(relOffset uint64) []Frame {
	fi := m.findFunc(relOffset)
	if fi < 0 {
		return nil
	}
	_, _, nameOff, inlineOff, inlineCount := m.funcAt(fi)
	funcName := m.str(nameOff)

	// Deepest source location at the offset (used for the innermost frame).
	locFile, locLine := m.lineLoc(relOffset)

	// Collect the inline records of this function that cover the offset, then
	// order them outermost..innermost by depth so we can assemble the chain.
	covering := m.coveringInlines(relOffset, inlineOff, inlineCount)

	frames := make([]Frame, 0, len(covering)+1)
	for k := len(covering) - 1; k >= 0; k-- {
		_, _, nOff, cFileOff, cLine, _ := m.inlineAt(covering[k])
		frames = append(frames, Frame{Function: m.str(nOff), File: locFile, Line: locLine})
		// The next (more-outer) frame is located at this inline's call site.
		locFile, locLine = m.str(cFileOff), cLine
	}
	frames = append(frames, Frame{Function: funcName, File: locFile, Line: locLine})
	return frames
}

// findFunc returns the index of the function covering rel, or -1.
func (m *Map) findFunc(rel uint64) int {
	// Greatest addrStart <= rel.
	lo, hi := 0, m.nFuncs
	for lo < hi {
		mid := (lo + hi) / 2
		s, _, _, _, _ := m.funcAt(mid)
		if s <= rel {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	i := lo - 1
	if i < 0 {
		return -1
	}
	_, end, _, _, _ := m.funcAt(i)
	if rel >= end {
		return -1
	}
	return i
}

// lineLoc returns the file:line of the row covering rel (greatest addr <= rel).
func (m *Map) lineLoc(rel uint64) (string, uint32) {
	lo, hi := 0, m.nLines
	for lo < hi {
		mid := (lo + hi) / 2
		a, _, _ := m.lineAt(mid)
		if a <= rel {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	i := lo - 1
	if i < 0 {
		return "", 0
	}
	_, fileOff, line := m.lineAt(i)
	return m.str(fileOff), line
}

// coveringInlines returns indices (into inlines[]) of this function's inline
// records that contain rel, ordered by depth ascending (outermost first).
func (m *Map) coveringInlines(rel uint64, off, count uint32) []int {
	if count == 0 {
		return nil
	}
	var out []int
	for j := uint32(0); j < count; j++ {
		idx := int(off + j)
		s, en, _, _, _, _ := m.inlineAt(idx)
		if rel >= s && rel < en {
			out = append(out, idx)
		}
	}
	// Records are stored depth-ascending within a func group, so out is already
	// ordered outermost..innermost; sort defensively in case a producer differs.
	sort.Slice(out, func(a, b int) bool {
		_, _, _, _, _, da := m.inlineAt(out[a])
		_, _, _, _, _, db := m.inlineAt(out[b])
		return da < db
	})
	return out
}

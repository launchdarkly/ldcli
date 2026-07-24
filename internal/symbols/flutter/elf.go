// Package flutter converts a Flutter/Dart AOT debug-symbols file
// (app.<platform>.symbols — an ELF containing DWARF) into the compact,
// mmap-friendly .dartmap symbol map the backend consumes. It is the ldcli-side
// (encode) half of the Flutter symbolication pipeline.
//
// A release Flutter build produced with `--obfuscate --split-debug-info=<dir>`
// strips Dart names and emits address-based crash frames plus a header carrying
// a build id. The matching per-arch .symbols ELF carries the same build id (as a
// GNU build-id note) and the DWARF needed to resolve those addresses back to
// Dart function + file:line, expanding inlined calls.
//
// This uses only the Go standard library (debug/elf + debug/dwarf) so ldcli
// cross-compiles cleanly, and reuses the dsymmap codec shared with Apple. Dart
// symbol names in the .symbols DWARF are already human-readable, so — unlike the
// Apple path — no demangling is applied.
package flutter

import (
	"debug/dwarf"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"path/filepath"
	"strings"

	e "github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/symbols/dsymmap"
)

// ntGNUBuildID is the ELF note type for a GNU build id (NT_GNU_BUILD_ID).
const ntGNUBuildID = 3

// Image is one .symbols file's recovered symbol map.
type Image struct {
	// SymbolsID is the Dart snapshot build id as lowercase hex — the Id-lane
	// lookup key (the same value the VM prints in every obfuscated crash header
	// and the backend recovers from it). Named symbols_id everywhere; it is not
	// exposed under the Dart "build id" term.
	SymbolsID string
	// Platform is the "<os>-<arch>" token (e.g. "android-arm64") parsed from the
	// app.<platform>.symbols filename — used for the Version-lane object name.
	Platform string
	Builder  *dsymmap.Builder
}

// BuildFromELF opens a Flutter app.<platform>.symbols ELF at path and returns
// its symbol map: the build id (symbols_id), platform token, and a dsymmap
// Builder the caller encodes to a .dartmap.
func BuildFromELF(path string) (Image, error) {
	f, err := elf.Open(path)
	if err != nil {
		return Image{}, e.Wrapf(err, "opening %s", path)
	}
	defer f.Close()

	symbolsID, err := readBuildID(f)
	if err != nil {
		return Image{}, e.Wrapf(err, "reading build id from %s", path)
	}

	d, err := f.DWARF()
	if err != nil {
		return Image{}, e.Wrapf(err, "reading DWARF from %s (was it built with --split-debug-info?)", path)
	}

	// Dart AOT addresses in the crash `virt` column are already snapshot-relative
	// and match the DWARF vaddr, so no rebasing is needed (TextVMAddr = 0).
	b := &dsymmap.Builder{}
	if err := populate(d, b); err != nil {
		return Image{}, err
	}

	return Image{
		SymbolsID: symbolsID,
		Platform:  platformFromFilename(path),
		Builder:   b,
	}, nil
}

// platformFromFilename extracts the "<os>-<arch>" token from an
// app.<platform>.symbols filename (e.g. "app.android-arm64.symbols" ->
// "android-arm64"). It returns "" when the name doesn't match, so the caller can
// fall back or surface a clear error; the Id lane does not depend on it.
func platformFromFilename(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".symbols")
	base = strings.TrimPrefix(base, "app.")
	if base == "" || strings.Contains(base, ".") {
		return ""
	}
	return base
}

// readBuildID returns the GNU build id of an ELF as lowercase hex, scanning both
// note sections and PT_NOTE segments (Dart emits it as a .note.gnu.build-id).
func readBuildID(f *elf.File) (string, error) {
	for _, sec := range f.Sections {
		if sec.Type != elf.SHT_NOTE {
			continue
		}
		data, err := sec.Data()
		if err != nil {
			continue
		}
		if id := buildIDFromNotes(data, f.ByteOrder); id != "" {
			return id, nil
		}
	}
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_NOTE {
			continue
		}
		data := make([]byte, prog.Filesz)
		if _, err := prog.ReadAt(data, 0); err != nil {
			continue
		}
		if id := buildIDFromNotes(data, f.ByteOrder); id != "" {
			return id, nil
		}
	}
	return "", e.New("no GNU build-id note found")
}

// buildIDFromNotes walks a note section/segment (a sequence of ELF notes) and
// returns the descriptor of the first NT_GNU_BUILD_ID note as lowercase hex, or
// "" if there is none. Each note is: namesz(4) descsz(4) type(4), then the name
// and descriptor, each padded to a 4-byte boundary.
func buildIDFromNotes(data []byte, bo binary.ByteOrder) string {
	for len(data) >= 12 {
		namesz := bo.Uint32(data[0:4])
		descsz := bo.Uint32(data[4:8])
		ntype := bo.Uint32(data[8:12])
		nameEnd := 12 + int(namesz)
		descStart := align4(nameEnd)
		descEnd := descStart + int(descsz)
		if descEnd > len(data) || nameEnd > len(data) {
			return ""
		}
		if ntype == ntGNUBuildID && descsz > 0 {
			return hex.EncodeToString(data[descStart:descEnd])
		}
		// Each note record is at least 12 bytes (header), so this always advances.
		data = data[align4(descEnd):]
	}
	return ""
}

func align4(n int) int { return (n + 3) &^ 3 }

// scope is one level of the DWARF DIE tree during the depth-first walk (see the
// Apple builder for the shared shape). fn is the nearest enclosing concrete
// function; inlineDepth counts inlined_subroutine ancestors.
type scope struct {
	fn          *dsymmap.Function
	inlineDepth uint32
}

// populate walks the DWARF DIE tree into b: physical functions, their line
// tables, and inlined-call chains. Mirrors apple.populate but on the standard
// library's debug/dwarf types.
func populate(d *dwarf.Data, b *dsymmap.Builder) error {
	r := d.Reader()
	var stack []scope
	var funcs []*dsymmap.Function
	var curFiles []*dwarf.LineFile

	top := func() scope {
		if len(stack) == 0 {
			return scope{}
		}
		return stack[len(stack)-1]
	}

	for {
		ent, err := r.Next()
		if err != nil {
			return e.Wrap(err, "walking DWARF")
		}
		if ent == nil {
			break
		}
		// A Tag==0 entry terminates the current sibling list: pop one scope.
		if ent.Tag == 0 {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}

		push := top()

		switch ent.Tag {
		case dwarf.TagCompileUnit:
			curFiles = filesForCU(d, ent)
			addLines(d, ent, b)

		case dwarf.TagSubprogram:
			if fn := makeFunction(d, ent); fn != nil {
				funcs = append(funcs, fn)
				push.fn = fn
			}
			push.inlineDepth = 0

		case dwarf.TagInlinedSubroutine:
			depth := top().inlineDepth + 1
			if fn := top().fn; fn != nil {
				fn.Inlines = append(fn.Inlines, makeInlines(d, ent, depth, curFiles)...)
			}
			push.inlineDepth = depth
		}

		if ent.Children {
			stack = append(stack, push)
		}
	}

	for _, fp := range funcs {
		b.Funcs = append(b.Funcs, *fp)
	}
	return nil
}

func makeFunction(d *dwarf.Data, ent *dwarf.Entry) *dsymmap.Function {
	lo, hi, ok := spanOf(d, ent)
	if !ok {
		return nil
	}
	return &dsymmap.Function{Start: lo, End: hi, Name: entryName(d, ent)}
}

func makeInlines(d *dwarf.Data, ent *dwarf.Entry, depth uint32, files []*dwarf.LineFile) []dsymmap.Inline {
	ranges, err := d.Ranges(ent)
	if err != nil || len(ranges) == 0 {
		return nil
	}
	name := entryName(d, ent)
	callFile, callLine := callSite(ent, files)

	out := make([]dsymmap.Inline, 0, len(ranges))
	for _, rng := range ranges {
		if rng[1] <= rng[0] {
			continue
		}
		out = append(out, dsymmap.Inline{
			Start:    rng[0],
			End:      rng[1],
			Name:     name,
			CallFile: callFile,
			CallLine: callLine,
			Depth:    depth,
		})
	}
	return out
}

// spanOf returns the [low,high) covering all of an entry's PC ranges.
func spanOf(d *dwarf.Data, ent *dwarf.Entry) (uint64, uint64, bool) {
	ranges, err := d.Ranges(ent)
	if err != nil || len(ranges) == 0 {
		return 0, 0, false
	}
	lo, hi := ranges[0][0], ranges[0][1]
	for _, rng := range ranges[1:] {
		if rng[0] < lo {
			lo = rng[0]
		}
		if rng[1] > hi {
			hi = rng[1]
		}
	}
	if hi <= lo {
		return 0, 0, false
	}
	return lo, hi, true
}

func callSite(ent *dwarf.Entry, files []*dwarf.LineFile) (string, uint32) {
	var line uint32
	if v, ok := ent.Val(dwarf.AttrCallLine).(int64); ok {
		line = uint32(v)
	}
	file := ""
	if v, ok := ent.Val(dwarf.AttrCallFile).(int64); ok {
		if idx := int(v); idx >= 0 && idx < len(files) && files[idx] != nil {
			file = files[idx].Name
		}
	}
	return file, line
}

func addLines(d *dwarf.Data, cu *dwarf.Entry, b *dsymmap.Builder) {
	lr, err := d.LineReader(cu)
	if err != nil || lr == nil {
		return
	}
	var le dwarf.LineEntry
	for {
		if err := lr.Next(&le); err != nil {
			break // io.EOF or malformed table: stop, keep what we have
		}
		if le.EndSequence {
			// Mark the end of a code range so a lookup in the following gap
			// resolves to no source location rather than the previous row.
			b.Lines = append(b.Lines, dsymmap.LineRow{Addr: le.Address})
			continue
		}
		file := ""
		if le.File != nil {
			file = le.File.Name
		}
		b.Lines = append(b.Lines, dsymmap.LineRow{Addr: le.Address, File: file, Line: uint32(le.Line)})
	}
}

func filesForCU(d *dwarf.Data, cu *dwarf.Entry) []*dwarf.LineFile {
	lr, err := d.LineReader(cu)
	if err != nil || lr == nil {
		return nil
	}
	return lr.Files()
}

// entryName resolves a subprogram/inlined_subroutine name, following
// abstract_origin/specification references. Dart DWARF names are already
// readable, so no demangling is applied.
func entryName(d *dwarf.Data, ent *dwarf.Entry) string {
	if name, _ := ent.Val(dwarf.AttrName).(string); name != "" {
		return name
	}
	if off, ok := refOffset(ent); ok {
		if name := resolveName(d, off, 0); name != "" {
			return name
		}
	}
	return "<unknown>"
}

func refOffset(ent *dwarf.Entry) (dwarf.Offset, bool) {
	if off, ok := ent.Val(dwarf.AttrAbstractOrigin).(dwarf.Offset); ok {
		return off, true
	}
	if off, ok := ent.Val(dwarf.AttrSpecification).(dwarf.Offset); ok {
		return off, true
	}
	return 0, false
}

func resolveName(d *dwarf.Data, off dwarf.Offset, depth int) string {
	if depth > 4 {
		return ""
	}
	r := d.Reader()
	r.Seek(off)
	ent, err := r.Next()
	if err != nil || ent == nil {
		return ""
	}
	if name, _ := ent.Val(dwarf.AttrName).(string); name != "" {
		return name
	}
	if next, ok := refOffset(ent); ok {
		return resolveName(d, next, depth+1)
	}
	return ""
}

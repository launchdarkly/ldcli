// Package apple converts an Apple dSYM (Mach-O + DWARF) into the compact,
// per-architecture .dsymmap symbol maps the backend consumes.
//
// It is the ldcli-side (encode) half of the Apple symbolication pipeline: it
// extracts each architecture's build UUID, walks the DWARF debug info to recover
// function ranges, line tables, and inlined-call chains, demangles Swift and C++
// names, and normalizes every address to be image-relative (file_addr −
// __TEXT.vmaddr) so it matches the rel_offset the device reports at crash time.
//
// Library choices (pure-Go so ldcli cross-compiles cleanly):
//   - github.com/blacktop/go-macho      Mach-O + fat parsing, UUID, __TEXT vmaddr
//   - github.com/blacktop/go-dwarf      DWARF line table + DIE walk (via go-macho)
//   - github.com/blacktop/go-macho/pkg/swift  Swift demangling
//   - github.com/ianlancetaylor/demangle      C/C++/Objective-C++ demangling
package apple

import (
	"encoding/hex"
	"os"
	"strings"

	"github.com/blacktop/go-dwarf"
	"github.com/blacktop/go-macho"
	"github.com/blacktop/go-macho/pkg/swift"
	"github.com/ianlancetaylor/demangle"
	e "github.com/pkg/errors"

	"github.com/launchdarkly/ldcli/internal/symbols/dsymmap"
)

// Arch is one architecture's symbol map recovered from a dSYM image.
type Arch struct {
	// UUID is the build UUID as 32 uppercase hex chars with no dashes — the
	// symbols_id lookup key the backend stores maps under (_sym/apple/id/<uuid>).
	UUID       string
	CPUType    uint32
	CPUSubtype uint32
	Builder    *dsymmap.Builder
}

// BuildFromMachO opens a dSYM's DWARF Mach-O image at path — thin or fat —
// and returns one symbol map per architecture. The caller encodes each
// Arch.Builder to its own .dsymmap keyed by Arch.UUID.
func BuildFromMachO(path string) ([]Arch, error) {
	// NewFatFile/NewFile read sections lazily from the ReaderAt, so f must stay
	// open through the DWARF walk below.
	f, err := os.Open(path)
	if err != nil {
		return nil, e.Wrapf(err, "opening %s", path)
	}
	defer f.Close()

	if fat, ferr := macho.NewFatFile(f); ferr == nil {
		out := make([]Arch, 0, len(fat.Arches))
		for i := range fat.Arches {
			a, err := buildArch(fat.Arches[i].File)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		return out, nil
	} else if ferr != macho.ErrNotFat {
		return nil, e.Wrapf(ferr, "reading fat header of %s", path)
	}

	mf, err := macho.NewFile(f)
	if err != nil {
		return nil, e.Wrapf(err, "reading Mach-O %s", path)
	}
	a, err := buildArch(mf)
	if err != nil {
		return nil, err
	}
	return []Arch{a}, nil
}

func buildArch(f *macho.File) (Arch, error) {
	uuidCmd := f.UUID()
	if uuidCmd == nil {
		return Arch{}, e.New("dSYM image has no LC_UUID load command")
	}
	uuid := [16]byte(uuidCmd.UUID)

	d, err := f.DWARF()
	if err != nil {
		return Arch{}, e.Wrap(err, "reading DWARF")
	}

	var textVM uint64
	if seg := f.Segment("__TEXT"); seg != nil {
		textVM = seg.Addr
	}

	b := &dsymmap.Builder{
		UUID:       uuid,
		TextVMAddr: textVM,
		CPUType:    uint32(f.CPU),
		CPUSubtype: uint32(f.SubCPU),
	}
	if err := populate(d, textVM, b); err != nil {
		return Arch{}, err
	}

	return Arch{
		UUID:       strings.ToUpper(hex.EncodeToString(uuid[:])),
		CPUType:    uint32(f.CPU),
		CPUSubtype: uint32(f.SubCPU),
		Builder:    b,
	}, nil
}

// scope is one level of the DWARF DIE tree during the depth-first walk. fn is
// the nearest enclosing concrete function (so inlined_subroutine DIEs know which
// function to attach to); inlineDepth is how many inlined_subroutine ancestors
// deep we are (0 inside the physical function body).
type scope struct {
	fn          *dsymmap.Function
	inlineDepth uint32
}

func populate(d *dwarf.Data, textVM uint64, b *dsymmap.Builder) error {
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

		push := top() // scope inherited by this entry's children unless changed below

		switch ent.Tag {
		case dwarf.TagCompileUnit:
			curFiles = filesForEntry(d, ent)
			addLines(d, ent, textVM, b)

		case dwarf.TagSubprogram:
			if fn := makeFunction(d, ent, textVM); fn != nil {
				funcs = append(funcs, fn)
				push.fn = fn
			}
			push.inlineDepth = 0

		case dwarf.TagInlinedSubroutine:
			depth := top().inlineDepth + 1
			if fn := top().fn; fn != nil {
				fn.Inlines = append(fn.Inlines, makeInlines(d, ent, textVM, depth, curFiles)...)
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

// makeFunction builds a single Function spanning a subprogram's PC ranges, or
// nil for a declaration / fully-inlined subprogram with no code of its own.
func makeFunction(d *dwarf.Data, ent *dwarf.Entry, textVM uint64) *dsymmap.Function {
	lo, hi, ok := spanOf(d, ent, textVM)
	if !ok {
		return nil
	}
	return &dsymmap.Function{Start: lo, End: hi, Name: entryName(d, ent)}
}

// makeInlines builds one Inline per PC range of an inlined_subroutine, all
// sharing the resolved callee name, call-site file:line, and nesting depth.
func makeInlines(d *dwarf.Data, ent *dwarf.Entry, textVM uint64, depth uint32, files []*dwarf.LineFile) []dsymmap.Inline {
	ranges, err := d.Ranges(ent)
	if err != nil || len(ranges) == 0 {
		return nil
	}
	name := entryName(d, ent)
	callFile, callLine := callSite(ent, files)

	out := make([]dsymmap.Inline, 0, len(ranges))
	for _, rng := range ranges {
		start, end, ok := relRange(rng[0], rng[1], textVM)
		if !ok {
			continue
		}
		out = append(out, dsymmap.Inline{
			Start:    start,
			End:      end,
			Name:     name,
			CallFile: callFile,
			CallLine: callLine,
			Depth:    depth,
		})
	}
	return out
}

// spanOf returns the image-relative [low,high) covering all of an entry's PC
// ranges (a single span; hot/cold split functions collapse to their extent).
func spanOf(d *dwarf.Data, ent *dwarf.Entry, textVM uint64) (uint64, uint64, bool) {
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
	return relRange(lo, hi, textVM)
}

// relRange rebases an absolute [start,end) file range to image-relative,
// dropping ranges that precede __TEXT or are empty.
func relRange(start, end, textVM uint64) (uint64, uint64, bool) {
	if end <= start || start < textVM {
		return 0, 0, false
	}
	return start - textVM, end - textVM, true
}

// callSite resolves an inlined_subroutine's DW_AT_call_file/call_line to a
// source file path and line in the caller.
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

func addLines(d *dwarf.Data, cu *dwarf.Entry, textVM uint64, b *dsymmap.Builder) {
	lr, err := d.LineReader(cu)
	if err != nil || lr == nil {
		return
	}
	var le dwarf.LineEntry
	for {
		if err := lr.Next(&le); err != nil {
			break // io.EOF or malformed table: stop, keep what we have
		}
		if le.Address < textVM {
			continue
		}
		rel := le.Address - textVM
		if le.EndSequence {
			// Mark the end of a code range so a lookup in the following gap
			// resolves to no source location rather than the previous row.
			b.Lines = append(b.Lines, dsymmap.LineRow{Addr: rel})
			continue
		}
		file := ""
		if le.File != nil {
			file = le.File.Name
		}
		b.Lines = append(b.Lines, dsymmap.LineRow{Addr: rel, File: file, Line: uint32(le.Line)})
	}
}

func filesForEntry(d *dwarf.Data, cu *dwarf.Entry) []*dwarf.LineFile {
	files, err := d.FilesForEntry(cu)
	if err != nil {
		return nil
	}
	return files
}

// entryName resolves the best human-readable name for a subprogram or
// inlined_subroutine, following abstract_origin/specification references and
// demangling Swift/C++ linkage names.
func entryName(d *dwarf.Data, ent *dwarf.Entry) string {
	name, _ := ent.Val(dwarf.AttrName).(string)
	linkage, _ := ent.Val(dwarf.AttrLinkageName).(string)
	if name == "" && linkage == "" {
		if off, ok := refOffset(ent); ok {
			name, linkage = resolveName(d, off, 0)
		}
	}
	return bestName(name, linkage)
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

func resolveName(d *dwarf.Data, off dwarf.Offset, depth int) (name, linkage string) {
	if depth > 4 {
		return "", ""
	}
	r := d.Reader()
	r.Seek(off)
	ent, err := r.Next()
	if err != nil || ent == nil {
		return "", ""
	}
	name, _ = ent.Val(dwarf.AttrName).(string)
	linkage, _ = ent.Val(dwarf.AttrLinkageName).(string)
	if name == "" && linkage == "" {
		if next, ok := refOffset(ent); ok {
			return resolveName(d, next, depth+1)
		}
	}
	return name, linkage
}

// bestName prefers a demangled linkage name (fuller: module/type-qualified) and
// falls back to the plain DW_AT_name, then the raw linkage, then a placeholder.
func bestName(name, linkage string) string {
	if linkage != "" {
		if swift.IsMangled(linkage) {
			if s, err := swift.Demangle(linkage); err == nil && s != "" {
				return s
			}
		}
		if s, ok := demangleCpp(linkage); ok {
			return s
		}
	}
	if name != "" {
		return name
	}
	if linkage != "" {
		return linkage
	}
	return "<unknown>"
}

func demangleCpp(sym string) (string, bool) {
	cand := sym
	if strings.HasPrefix(cand, "__Z") { // Mach-O adds a leading underscore
		cand = cand[1:]
	}
	if !strings.HasPrefix(cand, "_Z") {
		return "", false
	}
	out := demangle.Filter(cand)
	if out == "" || out == cand {
		return "", false
	}
	return out, true
}

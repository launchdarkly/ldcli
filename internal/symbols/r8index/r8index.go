// Package r8index reads and writes the symbolication data for an R8-obfuscated
// Android build: the `mapping.txt` R8 emits, and a random-access index over it.
//
// `ldcli symbols upload --type android` parses a build's mapping and uploads the
// index; the backend Android enhancer reads it. The two sides live in separate
// repos and duplicate this package rather than share a module, the way dsymmap and
// srcbundle already do: this directory is identical to
// backend/stacktraces/r8index in the observability repo apart from the srcbundle
// import path, and the version in the artifact's file name plus a golden file
// checked in on both sides guard against drift.
//
// Symbolication asks a mapping three things — retrace a frame, retrace a bare
// class name, name the file a class was compiled from — and one stack trace asks
// them about a few dozen classes. Answering out of the text means building all of
// it: a 61 MB release mapping becomes 10.7k classes, 50.5k method names and 480k
// member entries, retaining ~129 MB (the objects, plus the text their names alias)
// and leaving roughly a million pointers for every GC cycle to walk. An index
// answers the same three questions while decoding only the classes a trace
// mentions, holding ~5 MB and one small inflate per class.
//
// Unlike a JavaScript source map (file, line, column), R8 retrace works on (class,
// method, line): a JVM frame is "<fqcn>.<method>(File:line)" and carries no column.
//
// A Mapping (parsed text) and an Index (encoded) answer identically, and resolve a
// frame through the same code below — so what the round trip has to preserve is a
// class, not an answer.
package r8index

import "strings"

// Frame is one original frame a retrace resolved to. A single obfuscated frame can
// produce several, when R8 inlined callees into the method the device reported.
type Frame struct {
	Class  string
	Method string
	Line   int
	// Inlined marks a frame R8 recorded as an inlined callee rather than the
	// physical method the device reported. Such a frame's class — and therefore
	// its source file — is not the reported frame's, so nothing the device said
	// about the file applies to it.
	Inlined bool
	// Synthetic marks a frame belonging to code R8 generated. There is no source
	// file to show for it and nothing it explains, so it is reported to the UI as
	// background rather than dropped — a frame the device really executed.
	Synthetic bool
}

// class is one entry of a mapping, in the form both the parser and the decoder
// produce.
type class struct {
	originalName string
	// synthesized marks a class R8 generated rather than compiled from source,
	// per its class-level "com.android.tools.r8.synthesized" marker. Kotlin
	// lambdas desugar into one of these ("<Enclosing>$$ExternalSyntheticLambda7"),
	// and they show up in real stack traces even though no such class was written.
	synthesized bool
	// members is keyed by the *obfuscated* method name; a name can have several
	// entries (overloads, distinct line ranges, inlined callees).
	members map[string][]member
}

type member struct {
	hasRange bool
	minStart int
	minEnd   int
	// origClass is non-empty only for inlined frames, where R8 prefixes the
	// original signature with the class the inlined code came from.
	origClass  string
	origMethod string
	hasOrig    bool
	origStart  int
	hasOrigEnd bool
	origEnd    int
	// synthesized mirrors class.synthesized for a single member. R8 only writes
	// the marker on a member's *first* occurrence, so repeated entries for the
	// same synthetic method come through unmarked — which is why the class-level
	// flag, not this one, is what a frame's synthetic-ness is decided by.
	synthesized bool
}

// retrace resolves a frame within one class. Both forms of a mapping call this,
// which is the whole point of decoding a block into the same type the parser
// builds: only how the class was obtained differs.
func (c *class) retrace(obfMethod string, line int) []Frame {
	members := c.members[obfMethod]
	if len(members) == 0 {
		// Class known but method name not remapped (e.g. a kept/native method):
		// still deobfuscate the class.
		return []Frame{{
			Class:     c.originalName,
			Method:    obfMethod,
			Line:      line,
			Synthetic: c.synthesized,
		}}
	}

	// Prefer entries whose obfuscated line range covers the frame's line; these
	// carry the precise original line (and any inlining).
	var matched []member
	for _, mem := range members {
		if mem.hasRange && line >= mem.minStart && line <= mem.minEnd {
			matched = append(matched, mem)
		}
	}
	if len(matched) == 0 {
		// No ranged match: fall back to the first name mapping so the method is
		// still deobfuscated even without exact line info.
		return []Frame{c.frameFor(members[0], line)}
	}

	out := make([]Frame, 0, len(matched))
	for _, mem := range matched {
		out = append(out, c.frameFor(mem, mem.originalLine(line)))
	}
	return out
}

// frameFor builds the frame one member entry describes, at an already-resolved
// original line.
func (c *class) frameFor(mem member, line int) Frame {
	name, own := c.resolveClass(mem)
	return Frame{
		Class:     name,
		Method:    mem.origMethod,
		Line:      line,
		Inlined:   !own,
		Synthetic: mem.synthesized || (c.synthesized && own),
	}
}

// resolveClass returns the class a member entry belongs to, and whether that is
// the mapping entry's own class rather than an inlined callee's.
//
// A class writes its own methods bare and prefixes only the foreign classes it
// inlined, so a prefix normally means "a different class". A synthesized class is
// the exception: R8 prefixes even its own methods, and with the class's *residual*
// name ("g5.MainActivityKt$$ExternalSyntheticLambda10"), which carries the
// obfuscated package. The header's original name is the one worth reporting, so
// that a synthetic frame at least names the package it really came from.
func (c *class) resolveClass(mem member) (name string, own bool) {
	if mem.origClass == "" {
		return c.originalName, true
	}
	if c.synthesized && simpleClassName(mem.origClass) == simpleClassName(c.originalName) {
		return c.originalName, true
	}
	return mem.origClass, false
}

// simpleClassName drops the package from a (possibly nested) class name.
func simpleClassName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// originalLine maps an obfuscated line to the original line for a ranged member.
// A linear range (original span == obfuscated span) shifts by the offset; a
// collapsed range (inlined / single original line) maps everything to the start.
func (mem member) originalLine(line int) int {
	if !mem.hasOrig {
		return line
	}
	if mem.hasOrigEnd && (mem.origEnd-mem.origStart) == (mem.minEnd-mem.minStart) {
		return mem.origStart + (line - mem.minStart)
	}
	return mem.origStart
}

// isSourceFileName reports whether a name is a source file a JVM build compiles
// from, as opposed to one of R8's placeholders. Duplicated from the enhancer's
// copy so this package stays self-contained enough to be copied between repos.
func isSourceFileName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".java") || strings.HasSuffix(lower, ".kt")
}

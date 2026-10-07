package r8index

import (
	"strconv"
	"strings"
)

// Mapping is a whole mapping.txt parsed into memory. Encode turns one into an
// index, and symbolication reads the index instead — so in production this form
// only exists in the CLI, for as long as it takes to write one.
type Mapping struct {
	// classes is keyed by the *obfuscated* class name (dotted).
	classes map[string]*class
	// sourceFiles maps an *original* (deobfuscated) class name to the source file
	// it was compiled from, as recorded by R8's sourceFile metadata. Keyed by the
	// original name because that is what a retraced frame reports — and because an
	// inlined frame's class differs from the obfuscated class whose entry it was
	// found under.
	sourceFiles map[string]string
}

// Retrace resolves an obfuscated (class, method, line) to one or more original
// frames. Returns nil when the class isn't in the mapping (name wasn't obfuscated,
// or a stale mapping) so the caller can pass the frame through.
func (m *Mapping) Retrace(obfClass, obfMethod string, line int) []Frame {
	if m == nil {
		return nil
	}
	c := m.classes[obfClass]
	if c == nil {
		return nil
	}
	return c.retrace(obfMethod, line)
}

// RetraceClass deobfuscates a bare class name, with no method or line to narrow it
// down. An exception type needs exactly this: R8 renames a throwable's class like
// any other, so the reported type is obfuscated even though the class never has to
// appear in a frame. Reports false when the class isn't in the mapping, which is
// also how an unobfuscated name passes through untouched.
func (m *Mapping) RetraceClass(obfClass string) (string, bool) {
	if m == nil {
		return "", false
	}
	c := m.classes[obfClass]
	if c == nil || c.originalName == "" {
		return "", false
	}
	return c.originalName, true
}

// SourceFile reports the file an *original* class name was compiled from, per R8's
// metadata, or "" when the build recorded none.
func (m *Mapping) SourceFile(originalClass string) string {
	if m == nil {
		return ""
	}
	return m.sourceFiles[originalClass]
}

// Classes reports how many entries were parsed, which is how a caller tells a
// mapping it can symbolicate from one that parsed into nothing.
func (m *Mapping) Classes() int {
	if m == nil {
		return 0
	}
	return len(m.classes)
}

// Parse parses a whole mapping.txt into memory. Never returns nil: a file that is
// not a mapping at all reads as a mapping with no classes, which is what callers
// check.
func Parse(data []byte) *Mapping {
	m := &Mapping{classes: map[string]*class{}}
	// The first block wins a repeated obfuscated name, which is the one thing
	// EncodeFrom can do: srcbundle.Builder.Add ignores a key it already holds, and a
	// class it has already compressed into the bundle cannot be revisited. Plain
	// assignment here would keep the last instead, so one mapping would encode to two
	// different indexes depending on which encoder read it — and an index is
	// addressed by the id of the mapping it was built from.
	//
	// A name repeats when mapping files are concatenated: a build with feature splits,
	// or one that appends a module's mapping to another's.
	sc := newScanner(func(obf string, c *class) {
		if _, seen := m.classes[obf]; !seen {
			m.classes[obf] = c
		}
	})
	sc.scan(data)
	m.sourceFiles = sc.sourceFiles
	return m
}

// scanner reads the R8/ProGuard mapping.txt grammar, handing over each class as it
// completes. Grammar (per class):
//
//	<original.class.Name> -> <obf>:
//	    <fieldType> <fieldName> -> <obf>                 (ignored)
//	    <ret> <method>(<args>) -> <obf>                  (name only)
//	    <s>:<e>:<ret> [<origClass>.]<method>(<args>)[:<os>[:<oe>]] -> <obf>
//
// Handing classes over one at a time is what lets EncodeFrom write an index without
// ever holding the whole mapping (see encode.go). The source-file table is kept
// here rather than streamed: it is one small map, and both callers need all of it.
type scanner struct {
	onClass     func(obf string, c *class)
	sourceFiles map[string]string

	current    *class
	currentObf string
	// A comment annotates the entry directly above it, so the marker it carries has
	// to land on that entry: on the class while no member has been read yet, on a
	// member once one has, and on neither for the lines skipped below. That last
	// case is what keeps a synthesized *field* — ordinary classes have them — from
	// marking its whole class as R8-generated.
	//
	// The member is held as key + index rather than a pointer because appending to
	// the slice can move what a pointer refers to.
	commentTargetsClass bool
	lastMemberKey       string
	lastMemberIdx       int
}

func newScanner(onClass func(obf string, c *class)) *scanner {
	return &scanner{onClass: onClass, sourceFiles: map[string]string{}}
}

func (s *scanner) scan(data []byte) {
	for _, raw := range strings.Split(string(data), "\n") {
		s.line(raw)
	}
	s.flush()
}

// flush hands over the class in hand, which is complete once anything else starts.
func (s *scanner) flush() {
	if s.current != nil {
		s.onClass(s.currentObf, s.current)
		s.current, s.currentObf = nil, ""
	}
}

func (s *scanner) line(raw string) {
	line := strings.TrimRight(raw, "\r")
	if line == "" {
		return
	}
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		// Most comments are R8 bookkeeping, but two carry things nothing else
		// records: sourceFile is the only authoritative statement of which file a
		// class was compiled from (it can't be derived from the class name, see the
		// enhancer's androidKnownSourceFile), and the synthesized marker is the only
		// way to tell R8's own scaffolding from code somebody wrote.
		if s.current == nil {
			return
		}
		if f := parseSourceFileComment(trimmed); f != "" {
			if _, seen := s.sourceFiles[s.current.originalName]; !seen {
				s.sourceFiles[s.current.originalName] = f
			}
		}
		if parseSynthesizedComment(trimmed) {
			switch {
			case s.commentTargetsClass:
				s.current.synthesized = true
			case s.lastMemberKey != "":
				s.current.members[s.lastMemberKey][s.lastMemberIdx].synthesized = true
			}
		}
		return
	}
	if line[0] != ' ' && line[0] != '\t' {
		// Class header: "<original> -> <obf>:"
		s.flush()
		s.commentTargetsClass = false
		s.lastMemberKey = ""
		if orig, obf, ok := parseClassHeader(line); ok {
			s.current = &class{originalName: orig, members: map[string][]member{}}
			s.currentObf = obf
			s.commentTargetsClass = true
		}
		return
	}
	if s.current == nil {
		return
	}
	s.commentTargetsClass = false
	s.lastMemberKey = ""
	if obf, mem, ok := parseMember(strings.TrimSpace(line)); ok {
		s.current.members[obf] = append(s.current.members[obf], mem)
		s.lastMemberKey, s.lastMemberIdx = obf, len(s.current.members[obf])-1
	}
}

func parseClassHeader(line string) (original, obfuscated string, ok bool) {
	if !strings.HasSuffix(line, ":") {
		return "", "", false
	}
	body := strings.TrimSuffix(line, ":")
	arrow := strings.Index(body, " -> ")
	if arrow < 0 {
		return "", "", false
	}
	original = strings.TrimSpace(body[:arrow])
	obfuscated = strings.TrimSpace(body[arrow+len(" -> "):])
	if original == "" || obfuscated == "" {
		return "", "", false
	}
	return original, obfuscated, true
}

// parseMember parses a single (already-trimmed) member line. Field lines and
// anything without a method signature "(...)" are ignored (ok=false).
func parseMember(line string) (obfName string, mem member, ok bool) {
	arrow := strings.LastIndex(line, " -> ")
	if arrow < 0 {
		return "", member{}, false
	}
	left := line[:arrow]
	obfName = strings.TrimSpace(line[arrow+len(" -> "):])
	if obfName == "" {
		return "", member{}, false
	}

	// Optional leading "<minStart>:<minEnd>:" obfuscated line range.
	if s, e, rest, has := parseLeadingLineRange(left); has {
		mem.hasRange = true
		mem.minStart = s
		mem.minEnd = e
		left = rest
	}

	// left is now "<returnType> [<origClass>.]<method>(<args>)[:<os>[:<oe>]]".
	// Drop the return type (everything up to the first space).
	sp := strings.Index(left, " ")
	if sp < 0 {
		return "", member{}, false
	}
	sig := left[sp+1:]

	paren := strings.Index(sig, "(")
	if paren < 0 {
		// A field (no parens): not retraceable.
		return "", member{}, false
	}
	name := sig[:paren]

	// Trailing ":<origStart>[:<origEnd>]" after the closing paren.
	if closing := strings.Index(sig, ")"); closing >= 0 {
		if tail := sig[closing+1:]; strings.HasPrefix(tail, ":") {
			parseOriginalLineRange(tail[1:], &mem)
		}
	}

	// name may be "<origClass>.<method>" for inlined frames.
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		mem.origClass = name[:dot]
		mem.origMethod = name[dot+1:]
	} else {
		mem.origMethod = name
	}
	if mem.origMethod == "" {
		return "", member{}, false
	}
	return obfName, mem, true
}

// parseLeadingLineRange consumes a "<start>:<end>:" prefix if present.
func parseLeadingLineRange(s string) (start, end int, rest string, ok bool) {
	first := strings.Index(s, ":")
	if first <= 0 {
		return 0, 0, s, false
	}
	start, err := strconv.Atoi(s[:first])
	if err != nil {
		return 0, 0, s, false
	}
	after := s[first+1:]
	second := strings.Index(after, ":")
	if second <= 0 {
		return 0, 0, s, false
	}
	end, err = strconv.Atoi(after[:second])
	if err != nil {
		return 0, 0, s, false
	}
	return start, end, after[second+1:], true
}

// parseOriginalLineRange fills mem from a trailing "<origStart>[:<origEnd>]".
func parseOriginalLineRange(s string, mem *member) {
	parts := strings.Split(s, ":")
	if len(parts) == 0 {
		return
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return
	}
	mem.hasOrig = true
	mem.origStart = start
	if len(parts) > 1 {
		if end, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
			mem.hasOrigEnd = true
			mem.origEnd = end
		}
	}
}

// parseSourceFileComment extracts the file name from R8's sourceFile metadata
// comment, which sits directly under a class header:
//
//	# {"id":"sourceFile","fileName":"SymbolicationDemo.kt"}
//
// Returns "" for any other comment, and for R8's own placeholders — a synthetic
// class ("R8$$SyntheticClass") or a renamed one ("SourceFile") names no real file,
// so there is nothing to prefer over the name the enhancer derives.
func parseSourceFileComment(comment string) string {
	if !strings.Contains(comment, `"sourceFile"`) {
		return ""
	}
	const key = `"fileName":"`
	i := strings.Index(comment, key)
	if i < 0 {
		return ""
	}
	rest := comment[i+len(key):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	name := rest[:end]
	if !isSourceFileName(name) {
		return ""
	}
	return name
}

// parseSynthesizedComment reports whether a comment is R8's marker for an entry it
// generated itself rather than compiled from source:
//
//	# {"id":"com.android.tools.r8.synthesized"}
//
// R8 writes it under a class header for a wholly synthesized class (a desugared
// lambda), and under a member line for a synthesized member.
func parseSynthesizedComment(comment string) bool {
	return strings.Contains(comment, `"com.android.tools.r8.synthesized"`)
}

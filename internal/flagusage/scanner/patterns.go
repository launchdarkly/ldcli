package scanner

import sitter "github.com/smacker/go-tree-sitter"

// Language holds everything the scanner needs to handle a specific language:
// which tree-sitter grammar to use, which file extensions to collect,
// and how to extract flag references from AST nodes.
type Language struct {
	Name       string
	Extensions []string
	GetParser  func() *sitter.Language

	// ScanFile is pass 1: it finds direct SDK calls and *accessor definitions*
	// (kind "wrapper-definition", carrying a WrapperName) within a single file.
	ScanFile func(root *sitter.Node, src []byte, filePath string) []FlagReference

	// ResolveCalls is pass 2 (optional): given the accessor-name→flag-key map
	// built from every definition found across the scan, it finds call sites in
	// one file that invoke those accessors and emits resolved "wrapper-call"
	// references. Each language expresses "calling an accessor" differently — a
	// bare imported function in TS (`enableFoo()`), a method on a flags struct in
	// Go (`get.Flags(ctx).EnableFoo()`) — so the resolution lives per language
	// while the definition map is shared. Nil means the language has no pass-2.
	ResolveCalls func(rc ResolveContext) []FlagReference
}

// ResolveContext carries everything pass 2 needs to resolve accessor call sites
// in a single file. The same struct serves every language; fields a given
// language doesn't use (e.g. WrapperModules for Go) are simply ignored.
type ResolveContext struct {
	Root            *sitter.Node
	Src             []byte
	FilePath        string
	Lines           []string
	ExportToFlagKey map[string]string // accessor name -> flag key (shared across the scan)
	WrapperModules  map[string]bool   // modules whose imports are always treated as accessors (TS)
}

// VariationMethod describes an SDK method that evaluates a flag.
type VariationMethod struct {
	FlagKeyArg int // 0-based argument position of the flag key
	DefaultArg int // 0-based argument position of the default value (-1 if none)
}

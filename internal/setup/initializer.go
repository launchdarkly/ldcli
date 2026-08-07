package setup

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed sdk_init_templates/*.tmpl
var initTemplateFiles embed.FS

// InitConfig holds the values to interpolate into SDK initialization templates.
type InitConfig struct {
	SDKKey       string
	ClientSideID string
	MobileKey    string
	FlagKey      string
}

// InitResult describes the outcome of injecting SDK initialization code.
//
// Success is true only when initialization code was actually written to a file
// as valid, ready-to-run code. When Success is false, Snippet (if set) holds the
// rendered code the user must place manually, and DocsURL points at the setup
// guide.
type InitResult struct {
	SDKID    string `json:"sdk_id"`
	FilePath string `json:"file_path,omitempty"`
	DocsURL  string `json:"docs_url,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	// AlreadyInitialized reports that the entry file initialized the SDK before
	// this run, so nothing was written. Setup is complete either way, which is why
	// it accompanies Success.
	AlreadyInitialized bool `json:"already_initialized,omitempty"`
	Success            bool `json:"success"`
}

// appendSafeSDKs lists SDKs whose entry file is an interpreted script executed
// top-to-bottom, so initialization statements can be appended at file scope and
// still run. For every other SDK — compiled/scoped languages (Go, Java, C#,
// Swift, Android) whose statements are illegal at file scope, and framework SDKs
// (React, React Native) that must be wired into a component tree — appending
// produces code that does not compile or does not run, so we return the snippet
// as guidance instead of writing a broken file.
var appendSafeSDKs = map[string]bool{
	"node-server":       true,
	"python-server-sdk": true,
	"ruby-server-sdk":   true,
}

// defaultEntryPoints names the file to create for an SDK when there is no detected
// entry point to write into. Only the append-safe SDKs need one, since every other
// SDK returns a snippet and never touches the filesystem. The names match the
// fallbacks detection already suggests for these languages.
var defaultEntryPoints = map[string]string{
	"node-server":       "index.js",
	"python-server-sdk": "main.py",
	"ruby-server-sdk":   "main.rb",
}

// DefaultEntryPoint returns the file to create for sdkID when no entry point was
// detected for it, or an empty string when the SDK does not write to disk.
func DefaultEntryPoint(sdkID string) string {
	return defaultEntryPoints[sdkID]
}

// Initializer injects SDK initialization code into a target file.
type Initializer struct{}

// sdkTemplateInfo maps an SDK ID to the template filename.
type sdkTemplateInfo struct {
	TemplateFile string
	// ESMTemplateFile renders the same initialization with ESM import syntax, for
	// entry points where a CommonJS require would not run. Empty for SDKs whose
	// language has no module-system split.
	ESMTemplateFile string
}

var sdkTemplates = map[string]sdkTemplateInfo{
	"react-client-sdk":   {TemplateFile: "react-client-sdk.tmpl"},
	"react-native":       {TemplateFile: "react-native.tmpl"},
	"js-client-sdk":      {TemplateFile: "js-client-sdk.tmpl"},
	"swift-client-sdk":   {TemplateFile: "swift-client-sdk.tmpl"},
	"android":            {TemplateFile: "android.tmpl"},
	"android-client-sdk": {TemplateFile: "android.tmpl"},
	"java-server-sdk":    {TemplateFile: "java-server-sdk.tmpl"},
	"ruby-server-sdk":    {TemplateFile: "ruby-server-sdk.tmpl"},
	"go-server-sdk":      {TemplateFile: "go-server-sdk.tmpl"},
	"python-server-sdk":  {TemplateFile: "python-server-sdk.tmpl"},
	"dotnet-server-sdk":  {TemplateFile: "dotnet-server-sdk.tmpl"},
	"node-server":        {TemplateFile: "node-server.tmpl", ESMTemplateFile: "node-server-esm.tmpl"},
}

// sdkDocsPaths maps SDK IDs to their documentation path on launchdarkly.com/docs.
// Covers all SDKs, including those without init templates.
var sdkDocsPaths = map[string]string{
	"akamai-server-edgekv-sdk": "sdk/edge/akamai",
	"android":                  "sdk/client-side/android",
	"android-client-sdk":       "sdk/client-side/android",
	"apex-server-sdk":          "sdk/server-side/apex",
	"cpp-client-sdk":           "sdk/client-side/c-c--",
	"cpp-server-sdk":           "sdk/server-side/c-c--",
	"cloudflare-server-sdk":    "sdk/edge/cloudflare",
	"dotnet-client-sdk":        "sdk/client-side/dotnet",
	"dotnet-server-sdk":        "sdk/server-side/dotnet",
	"electron-client-sdk":      "sdk/client-side/electron",
	"erlang-server-sdk":        "sdk/server-side/erlang",
	"flutter-client-sdk":       "sdk/client-side/flutter",
	"go-server-sdk":            "sdk/server-side/go",
	"haskell-server-sdk":       "sdk/server-side/haskell",
	"ios-client-sdk":           "sdk/client-side/ios",
	"swift-client-sdk":         "sdk/client-side/ios",
	"java-server-sdk":          "sdk/server-side/java",
	"js-client-sdk":            "sdk/client-side/javascript",
	"lua-server-sdk":           "sdk/server-side/lua",
	"node-client-sdk":          "sdk/client-side/node-js",
	"node-server":              "sdk/server-side/node-js",
	"node-server-sdk":          "sdk/server-side/node-js",
	"php-server-sdk":           "sdk/server-side/php",
	"python-server-sdk":        "sdk/server-side/python",
	"react-client-sdk":         "sdk/client-side/react",
	"react-native":             "sdk/client-side/react-native",
	"react-native-client-sdk":  "sdk/client-side/react-native",
	"roku-client-sdk":          "sdk/client-side/roku",
	"ruby-server-sdk":          "sdk/server-side/ruby",
	"rust-server-sdk":          "sdk/server-side/rust",
	"vercel-server-sdk":        "sdk/edge/vercel",
	"vue-client-sdk":           "sdk/client-side/vue",
}

const docsBaseURL = "https://launchdarkly.com/docs"

// GetDocsURL returns the full documentation URL for the given SDK ID.
// Falls back to the top-level SDK docs page if the ID is unknown.
func GetDocsURL(sdkID string) string {
	if path, ok := sdkDocsPaths[sdkID]; ok {
		return docsBaseURL + "/" + path
	}
	return docsBaseURL + "/sdk"
}

// SupportedSDKIDs returns the list of SDK IDs that have initialization templates.
func SupportedSDKIDs() []string {
	ids := make([]string, 0, len(sdkTemplates))
	for id := range sdkTemplates {
		ids = append(ids, id)
	}
	return ids
}

// HasTemplate returns true if the given SDK ID has an initialization template.
func HasTemplate(sdkID string) bool {
	_, ok := sdkTemplates[sdkID]
	return ok
}

// InjectsInPlace reports whether `init` writes runnable code directly into the
// entry file (true) versus returning a snippet for the user to place manually
// (false). Also indicates whether a live verify step is meaningful afterward.
func InjectsInPlace(sdkID string) bool {
	return HasTemplate(sdkID) && appendSafeSDKs[sdkID]
}

// UsesClientSideID reports whether an SDK authenticates with the environment's
// client-side ID rather than a server SDK key or a mobile key. It is derived from
// the SDK's own init template, which is the one place that already knows which
// credential the SDK takes, so a new template cannot disagree with a list here.
func UsesClientSideID(sdkID string) bool {
	const sentinel = "__ld_client_side_id_probe__"
	rendered, err := RenderTemplate(sdkID, InitConfig{ClientSideID: sentinel})
	if err != nil {
		return false
	}
	return strings.Contains(rendered, sentinel)
}

// RenderTemplate renders the initialization code for the given SDK, using the
// CommonJS form where an SDK has both. Prefer RenderTemplateForEntry when the
// target file is known, so the module syntax matches it.
func RenderTemplate(sdkID string, cfg InitConfig) (string, error) {
	return renderTemplate(sdkID, cfg, false)
}

// RenderTemplateForEntry renders the initialization code for the given SDK in the
// module syntax that runs in entryPath.
func RenderTemplateForEntry(sdkID, entryPath string, cfg InitConfig) (string, error) {
	return renderTemplate(sdkID, cfg, entryNeedsESM(entryPath))
}

func renderTemplate(sdkID string, cfg InitConfig, esm bool) (string, error) {
	info, ok := sdkTemplates[sdkID]
	if !ok {
		return "", fmt.Errorf("no initialization template for SDK %q; see docs: %s", sdkID, GetDocsURL(sdkID))
	}

	templateFile := info.TemplateFile
	if esm && info.ESMTemplateFile != "" {
		templateFile = info.ESMTemplateFile
	}

	content, err := initTemplateFiles.ReadFile("sdk_init_templates/" + templateFile)
	if err != nil {
		return "", fmt.Errorf("reading template for %s: %w", sdkID, err)
	}

	tmpl, err := template.New(sdkID).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parsing template for %s: %w", sdkID, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("executing template for %s: %w", sdkID, err)
	}

	return buf.String(), nil
}

// InjectIntoFile renders the SDK initialization code and, for SDKs whose entry
// file is an interpreted script (see appendSafeSDKs), writes it into filePath:
// imports are placed at the top and init code appended after existing content.
//
// For SDKs that are not append-safe — because file-scope statements would not
// compile (Go, Java, C#, Swift, Android) or because the code must be wired into
// a component tree (React, React Native) — the file is left untouched and the
// result carries the rendered Snippet plus DocsURL as guidance, with
// Success=false so callers do not report a broken file as ready.
//
// If no template exists for the SDK at all, the result carries only the
// documentation URL.
//
// The template output is split into an IMPORTS section and an INIT section by a
// separator line ("// --- init ---" or "# --- init ---" depending on language).
func (i Initializer) InjectIntoFile(sdkID, filePath string, cfg InitConfig) (*InitResult, error) {
	if !HasTemplate(sdkID) {
		return &InitResult{
			SDKID:   sdkID,
			DocsURL: GetDocsURL(sdkID),
			Success: false,
		}, nil
	}

	rendered, err := RenderTemplateForEntry(sdkID, filePath, cfg)
	if err != nil {
		return nil, err
	}

	importSection, initSection := splitInitSections(rendered)

	if !appendSafeSDKs[sdkID] {
		return &InitResult{
			SDKID:    sdkID,
			FilePath: filePath,
			DocsURL:  GetDocsURL(sdkID),
			Snippet:  joinSnippet(importSection, initSection),
			Success:  false,
		}, nil
	}

	existing, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			var content string
			if importSection != "" {
				content = importSection + "\n\n" + initSection + "\n"
			} else {
				content = initSection + "\n"
			}
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				return nil, fmt.Errorf("creating %s: %w", filePath, err)
			}
			return &InitResult{SDKID: sdkID, FilePath: filePath, Success: true}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", filePath, err)
	}

	content := string(existing)
	if alreadyInitialized(content, importSection, initSection) {
		return &InitResult{
			SDKID:              sdkID,
			FilePath:           filePath,
			AlreadyInitialized: true,
			Success:            true,
		}, nil
	}

	if importSection != "" {
		prologue, body := splitPrologue(sdkID, content)
		content = prologue + importSection + "\n" + body
	}
	content = content + "\n\n" + initSection + "\n"

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", filePath, err)
	}

	return &InitResult{SDKID: sdkID, FilePath: filePath, Success: true}, nil
}

// alreadyInitialized reports whether the file already contains the initialization
// this template would add. Injection appends at file scope, so a second copy
// redeclares the same names: in Node that is a SyntaxError that stops the app from
// starting, and in Python and Ruby it silently rebinds the client. Matching the
// template's own import lines keeps the test in whatever language the file is
// written in, and matching any one of them errs toward leaving a half-configured
// file alone rather than appending into it.
func alreadyInitialized(content, importSection, initSection string) bool {
	section := importSection
	if strings.TrimSpace(section) == "" {
		section = initSection
	}
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(content, line) {
			return true
		}
	}
	return false
}

// entryNeedsESM reports whether code written into entryPath has to use ESM import
// syntax. The extension decides it outright for the explicit cases; a plain .js
// entry depends on the enclosing package's "type" field. Detection points Node
// projects at TypeScript and ESM entry points such as Next.js instrumentation.ts
// and NestJS src/main.ts, where a CommonJS require does not run.
func entryNeedsESM(entryPath string) bool {
	switch strings.ToLower(filepath.Ext(entryPath)) {
	case ".mjs", ".mts", ".ts", ".tsx":
		return true
	case ".cjs", ".cts":
		return false
	}
	return packageIsESM(entryPath)
}

// packageIsESM reports whether the nearest package.json above entryPath declares
// "type": "module", which makes every plain .js file in the package ESM.
func packageIsESM(entryPath string) bool {
	dir := filepath.Dir(entryPath)
	for {
		content, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err == nil {
			var pkg struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(content, &pkg) == nil {
				return pkg.Type == "module"
			}
			return false
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// splitPrologue peels off the leading lines that have to stay above injected
// imports. Every language keeps its shebang, since anything above it stops the file
// being executable, and its leading comment block, which is the only place Python
// encoding cookies (PEP 263) and Ruby magic comments like frozen_string_literal are
// read. Python additionally keeps its module docstring, which is demoted to a plain
// expression if anything precedes it, and its __future__ imports, which are a
// SyntaxError below other code. CommonJS keeps a 'use strict' directive, which is
// ignored unless it is the first statement.
func splitPrologue(sdkID, content string) (prologue, rest string) {
	lines := splitLines(content)

	end := 0
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
		end = 1
	}
	end = skipCommentHeader(lines, end)

	switch sdkID {
	case "python-server-sdk":
		end = skipPythonHeader(lines, end)
	case "node-server":
		end = skipUseStrict(lines, end)
	}

	prologue = strings.Join(lines[:end], "")
	rest = strings.Join(lines[end:], "")
	if prologue != "" && !strings.HasSuffix(prologue, "\n") {
		prologue += "\n"
	}
	return prologue, rest
}

// skipCommentHeader advances past blank lines and comments, including the /* */
// block a license or JSDoc header usually opens with.
func skipCommentHeader(lines []string, i int) int {
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		switch {
		case t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//"):
			i++
		case strings.HasPrefix(t, "/*"):
			for i < len(lines) && !strings.Contains(lines[i], "*/") {
				i++
			}
			if i < len(lines) {
				i++
			}
		default:
			return i
		}
	}
	return i
}

// pythonStringStart matches the opening quote of a module docstring, allowing the
// string prefixes Python permits before it.
var pythonStringStart = regexp.MustCompile(`^[rRuUbBfF]{0,2}("""|'''|"|')`)

// skipPythonHeader advances past a module docstring and any __future__ imports,
// along with the comments and blank lines between them.
func skipPythonHeader(lines []string, i int) int {
	docstringSeen := false
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		switch {
		case t == "" || strings.HasPrefix(t, "#"):
			i++
		case strings.HasPrefix(t, "from __future__ import"):
			i = skipStatement(lines, i)
		case !docstringSeen && pythonStringStart.MatchString(t):
			docstringSeen = true
			quote := pythonStringStart.FindStringSubmatch(t)[1]
			body := t[strings.Index(t, quote)+len(quote):]
			i++
			if strings.Contains(body, quote) {
				continue // the docstring opened and closed on one line
			}
			for i < len(lines) && !strings.Contains(lines[i], quote) {
				i++
			}
			if i < len(lines) {
				i++
			}
		default:
			return i
		}
	}
	return i
}

// skipStatement advances past a statement that may continue over several lines with
// parentheses or a trailing backslash.
func skipStatement(lines []string, i int) int {
	depth := 0
	for i < len(lines) {
		line := strings.TrimRight(lines[i], "\n")
		depth += strings.Count(line, "(") - strings.Count(line, ")")
		continued := strings.HasSuffix(line, `\`)
		i++
		if depth <= 0 && !continued {
			break
		}
	}
	return i
}

// skipUseStrict advances past a 'use strict' directive.
func skipUseStrict(lines []string, i int) int {
	if i >= len(lines) {
		return i
	}
	directive := lines[i]
	if j := strings.Index(directive, "//"); j >= 0 {
		directive = directive[:j]
	}
	if j := strings.Index(directive, "/*"); j >= 0 {
		directive = directive[:j]
	}
	switch strings.TrimSuffix(strings.TrimSpace(directive), ";") {
	case `'use strict'`, `"use strict"`:
		return i + 1
	}
	return i
}

// splitLines splits s into lines, keeping each newline with the line it ends.
func splitLines(s string) []string {
	var lines []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return append(lines, s)
		}
		lines = append(lines, s[:i+1])
		s = s[i+1:]
	}
	return lines
}

// joinSnippet recombines the import and init sections into a single human-readable
// snippet the user can copy into the correct place in their code.
func joinSnippet(importSection, initSection string) string {
	if importSection == "" {
		return initSection
	}
	return importSection + "\n\n" + initSection
}

// initSeparators lists the markers that divide import and init sections in templates.
var initSeparators = []string{
	"// --- init ---",
	"# --- init ---",
}

// splitInitSections splits rendered template output into an import section and an
// init section. It recognises comment-style-appropriate separators so that templates
// for languages like Python and Ruby can use `#` comments.
func splitInitSections(rendered string) (importSection, initSection string) {
	for _, sep := range initSeparators {
		if parts := strings.SplitN(rendered, sep, 2); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	return "", rendered
}

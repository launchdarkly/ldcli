package setup

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
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
type InitResult struct {
	SDKID    string `json:"sdk_id"`
	FilePath string `json:"file_path,omitempty"`
	DocsURL  string `json:"docs_url,omitempty"`
	Success  bool   `json:"success"`
}

// Initializer injects SDK initialization code into a target file.
type Initializer struct{}

// sdkTemplateInfo maps an SDK ID to the template filename.
type sdkTemplateInfo struct {
	TemplateFile string
}

var sdkTemplates = map[string]sdkTemplateInfo{
	"react-client-sdk":  {TemplateFile: "react-client-sdk.tmpl"},
	"react-native":      {TemplateFile: "react-native.tmpl"},
	"js-client-sdk":     {TemplateFile: "js-client-sdk.tmpl"},
	"swift-client-sdk":  {TemplateFile: "swift-client-sdk.tmpl"},
	"android":           {TemplateFile: "android.tmpl"},
	"java-server-sdk":   {TemplateFile: "java-server-sdk.tmpl"},
	"ruby-server-sdk":   {TemplateFile: "ruby-server-sdk.tmpl"},
	"go-server-sdk":     {TemplateFile: "go-server-sdk.tmpl"},
	"python-server-sdk": {TemplateFile: "python-server-sdk.tmpl"},
	"dotnet-server-sdk": {TemplateFile: "dotnet-server-sdk.tmpl"},
	"node-server":       {TemplateFile: "node-server.tmpl"},
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

// RenderTemplate renders the initialization code for the given SDK.
func RenderTemplate(sdkID string, cfg InitConfig) (string, error) {
	info, ok := sdkTemplates[sdkID]
	if !ok {
		return "", fmt.Errorf("no initialization template for SDK %q; see docs: %s", sdkID, GetDocsURL(sdkID))
	}

	content, err := initTemplateFiles.ReadFile("sdk_init_templates/" + info.TemplateFile)
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

// InjectIntoFile reads the file at filePath, injects the rendered SDK initialization code,
// and writes the result back. If no template exists for the SDK, it returns a result with
// the documentation URL instead of an error.
//
// The template output is split into an IMPORTS section and an INIT section by a
// separator line ("// --- init ---" or "# --- init ---" depending on language).
// Imports are placed at the top of the file, and init code is appended after the
// existing content.
func (i Initializer) InjectIntoFile(sdkID, filePath string, cfg InitConfig) (*InitResult, error) {
	if !HasTemplate(sdkID) {
		return &InitResult{
			SDKID:   sdkID,
			DocsURL: GetDocsURL(sdkID),
			Success: false,
		}, nil
	}

	rendered, err := RenderTemplate(sdkID, cfg)
	if err != nil {
		return nil, err
	}

	importSection, initSection := splitInitSections(rendered)

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

	if importSection != "" {
		content = importSection + "\n" + content
	}
	content = content + "\n\n" + initSection + "\n"

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", filePath, err)
	}

	return &InitResult{SDKID: sdkID, FilePath: filePath, Success: true}, nil
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

package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// providerKeyNames are the environment variables that hold the API keys of
// the model providers that LaunchDarkly AI SDKs support.
var providerKeyNames = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"AZURE_OPENAI_API_KEY",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"MISTRAL_API_KEY",
	"COHERE_API_KEY",
	"GROQ_API_KEY",
	"AWS_ACCESS_KEY_ID",
	"AWS_PROFILE",
}

// ProviderKeys returns the names of the provider keys that are set, in the
// environment or in the project's .env file. It never returns a value. The
// agent uses the answer to decide whether it can run the app, so it never
// has to ask for a key or print the environment.
func ProviderKeys(dir string, environ []string) []string {
	set := map[string]bool{}
	for _, kv := range environ {
		if name, value, ok := strings.Cut(kv, "="); ok && strings.TrimSpace(value) != "" {
			set[name] = true
		}
	}
	for name := range dotEnvNames(filepath.Join(dir, ".env")) {
		set[name] = true
	}
	var found []string
	for _, name := range providerKeyNames {
		if set[name] {
			found = append(found, name)
		}
	}
	return found
}

// dotEnvNames returns the names in a .env file that have a value.
func dotEnvNames(path string) map[string]bool {
	names := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		return names
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "export ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		value = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), `"'`))
		if ok && value != "" {
			names[strings.TrimSpace(name)] = true
		}
	}
	return names
}

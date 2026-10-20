package agent

import (
	"fmt"
	"strings"
)

// Credential environment variables. The agent writes code that reads the
// credential from one of these, so the credential never goes into a prompt or
// into the user's source files.
const (
	SDKKeyEnvVar       = "LAUNCHDARKLY_SDK_KEY"
	ClientSideIDEnvVar = "LAUNCHDARKLY_CLIENT_SIDE_ID"
	MobileKeyEnvVar    = "LAUNCHDARKLY_MOBILE_KEY"
)

// clientSideSDKs read the client-side ID, and mobileSDKs read the mobile key.
// Every other SDK reads the server-side SDK key. The lists include each ID
// and alias that the installer and the templates accept.
var (
	clientSideSDKs = map[string]bool{
		"js-client-sdk":                 true,
		"launchdarkly-js-client-sdk":    true,
		"react-client-sdk":              true,
		"launchdarkly-react-client-sdk": true,
		"vue-client-sdk":                true,
		"node-client-sdk":               true,
		"electron-client-sdk":           true,
	}
	mobileSDKs = map[string]bool{
		"android":                 true,
		"android-client-sdk":      true,
		"ios-client-sdk":          true,
		"swift-client-sdk":        true,
		"react-native":            true,
		"react-native-client-sdk": true,
		"flutter-client-sdk":      true,
		"dotnet-client-sdk":       true,
		"cpp-client-sdk":          true,
		"roku-client-sdk":         true,
	}
)

// flutterSDK reads the mobile key on mobile platforms and the client-side ID on
// the web. The SDK ID does not give the platform, so the prompt names both.
const flutterSDK = "flutter-client-sdk"

// CredentialEnvVar returns the environment variable that the agent's code
// must read for the SDK.
func CredentialEnvVar(sdkID string) string {
	switch {
	case clientSideSDKs[sdkID]:
		return ClientSideIDEnvVar
	case mobileSDKs[sdkID]:
		return MobileKeyEnvVar
	}
	return SDKKeyEnvVar
}

// DefaultInstructions is the system prompt that the built-in configuration
// uses.
const DefaultInstructions = `You are the LaunchDarkly setup agent. You add LaunchDarkly SDK initialization code to the project in the current directory.

Follow these rules:
- Change only files inside the project directory.
- Make the smallest change that initializes the SDK where the application starts, and evaluates the given flag once.
- Read the SDK credential from an environment variable. Never write a key, an ID, or a token into a file.
- Do not install packages unless the task says that the SDK is not installed. Do not add other dependencies.
- Do not commit, push, or change git configuration.
- Follow the code style of the project.
- When you finish, list the files that you changed and say in one sentence how the user can see the flag value.`

// BuildFacts returns the first user message when the instructions come from
// the AI Config. It gives only the facts that the CLI detected and the choices
// that it made earlier in setup. It gives no task, so the config's
// instructions decide what the agent does.
func BuildFacts(t Task) string {
	var b strings.Builder
	b.WriteString("Follow your instructions to set up LaunchDarkly in the project in the current directory.\n\n")
	b.WriteString("Facts from ldcli setup:\n")
	writeFact(&b, "SDK", t.SDKID)
	writeFact(&b, "Language", t.Language)
	writeFact(&b, "Framework", t.Framework)
	writeFact(&b, "Package manager", t.PackageManager)
	writeFact(&b, "Entry point", t.EntryPoint)
	installed := "no"
	if t.SDKInstalled {
		installed = "yes"
	}
	writeFact(&b, "SDK package installed", installed)
	writeFact(&b, "LaunchDarkly project", t.ProjectKey)
	writeFact(&b, "LaunchDarkly environment", t.EnvironmentKey)
	writeFact(&b, "Flag created by setup", t.FlagKey)
	writeFact(&b, "Environment variable for the SDK credential", CredentialEnvVar(t.SDKID))
	writeFact(&b, "Model provider keys that are set", providerKeysFact(t.ProviderKeys))
	if t.SDKID == flutterSDK {
		writeFact(&b, "Environment variable for a Flutter web app", ClientSideIDEnvVar)
	}
	b.WriteString("\n" + setupRules)
	return b.String()
}

// setupRules ends both prompts, so the agent gets the same rules with or
// without the AI Config.
const setupRules = "ldcli gives the user the SDK credential. Do not look for keys, print the environment, or ask the user for a key. Use your temporary folder in $TMPDIR, not /tmp.\n"

func providerKeysFact(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ") + " (names only)"
}

// BuildPrompt returns the task message for one run.
func BuildPrompt(t Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Set up the LaunchDarkly SDK %q in this project.\n\n", t.SDKID)

	b.WriteString("Facts that ldcli detected:\n")
	writeFact(&b, "Language", t.Language)
	writeFact(&b, "Framework", t.Framework)
	writeFact(&b, "Package manager", t.PackageManager)
	writeFact(&b, "Entry point", t.EntryPoint)
	writeFact(&b, "LaunchDarkly project", t.ProjectKey)
	writeFact(&b, "LaunchDarkly environment", t.EnvironmentKey)
	writeFact(&b, "Model provider keys that are set", providerKeysFact(t.ProviderKeys))
	b.WriteString("\n")

	if t.SDKInstalled {
		b.WriteString("The SDK package is already installed. Do not install it again.\n")
	} else {
		b.WriteString("The SDK package is not installed. Install it with the project's package manager.\n")
	}
	fmt.Fprintf(&b, "Evaluate the boolean flag %q once after the SDK initializes, and print or log its value.\n", t.FlagKey)
	fmt.Fprintf(&b, "Read the credential from the environment variable %s.\n", CredentialEnvVar(t.SDKID))
	if t.SDKID == flutterSDK {
		fmt.Fprintf(&b, "If the app runs on the web, read the client-side ID from %s instead.\n", ClientSideIDEnvVar)
	}
	if t.EntryPoint == "" {
		b.WriteString("ldcli did not find an entry point. Find where the application starts before you edit.\n")
	}
	b.WriteString(setupRules)
	return b.String()
}

func writeFact(b *strings.Builder, name, value string) {
	if value == "" {
		value = "unknown"
	}
	fmt.Fprintf(b, "- %s: %s\n", name, value)
}

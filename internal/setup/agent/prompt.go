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
// Every other SDK reads the server-side SDK key.
var (
	clientSideSDKs = map[string]bool{"react-client-sdk": true, "js-client-sdk": true}
	mobileSDKs     = map[string]bool{"react-native": true, "android": true, "swift-client-sdk": true}
)

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

// BuildPrompt returns the task message for one run.
func BuildPrompt(t Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Set up the LaunchDarkly SDK %q in this project.\n\n", t.SDKID)

	b.WriteString("Facts that ldcli detected:\n")
	writeFact(&b, "Language", t.Language)
	writeFact(&b, "Framework", t.Framework)
	writeFact(&b, "Package manager", t.PackageManager)
	writeFact(&b, "Entry point", t.EntryPoint)
	b.WriteString("\n")

	if t.SDKInstalled {
		b.WriteString("The SDK package is already installed. Do not install it again.\n")
	} else {
		b.WriteString("The SDK package is not installed. Install it with the project's package manager.\n")
	}
	fmt.Fprintf(&b, "Evaluate the boolean flag %q once after the SDK initializes, and print or log its value.\n", t.FlagKey)
	fmt.Fprintf(&b, "Read the credential from the environment variable %s.\n", CredentialEnvVar(t.SDKID))
	if t.EntryPoint == "" {
		b.WriteString("ldcli did not find an entry point. Find where the application starts before you edit.\n")
	}
	return b.String()
}

func writeFact(b *strings.Builder, name, value string) {
	if value == "" {
		value = "unknown"
	}
	fmt.Fprintf(b, "- %s: %s\n", name, value)
}

package prompt_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncprompt "github.com/launchdarkly/ldcli/internal/sync/prompt"
)

// requireNoSettingsWrite checks that sync never wrote the sync settings:
// LaunchDarkly records code as the owner from the synced manifest.
func requireNoSettingsWrite(t *testing.T, requests []string) {
	t.Helper()
	assert.Zero(t, countRequests(requests, http.MethodPatch, "/configs/sync/settings"), "no sync settings write")
}

// managedVariationWithLocalChange returns a working copy that tracks the
// managed variation support/default, with a local change to push.
func managedVariationWithLocalChange(t *testing.T) (string, *directAPI) {
	t.Helper()
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), true)
	writeManifest(t, root, baseline)
	return root, &directAPI{variation: pointer(baseline)}
}

// linkPrompt links prompt.md to support/default and pushes the local copy.
func linkPrompt(t *testing.T, root string, api *directAPI) (string, error) {
	t.Helper()
	_, notes, err := runPrompt(t, root, api, "link", "--file", "prompt.md", "--format", "markdown",
		"--project", "production", "--config-key", "support", "--key", "default",
		"--conflict", "local", "--yes", "--no-input")
	return notes, err
}

const (
	supportDefaultSetting = "variation:support/default"
	managedMessage        = "support/default is managed in LaunchDarkly; pulled, not pushed. " +
		"To manage it in code, switch it to Code in LaunchDarkly."
)

func managedInLaunchDarkly(api *directAPI) {
	api.directions = map[string]syncdomain.SyncDirection{supportDefaultSetting: syncdomain.SyncDirectionServerCanonical}
	api.patterns = map[string]string{supportDefaultSetting: "support/default"}
}

// A variation that no rule matches resolves to the server_canonical default,
// which does not make it pull: the push writes it, and writes no rule.
func TestPushOfAVariationWithoutARuleWritesNoSyncSettings(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	requireNoSettingsWrite(t, api.requests)
	assert.Equal(t, 1, countRequests(api.requests, http.MethodPatch, "/variations/default"))
	assert.Equal(t, "Local", api.variation.Name)
}

// A variation that code owns is pushed.
func TestPushOfACodeOwnedVariation(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.directions = map[string]syncdomain.SyncDirection{supportDefaultSetting: syncdomain.SyncDirectionCodeCanonical}
	api.patterns = map[string]string{supportDefaultSetting: "support/default"}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	requireNoSettingsWrite(t, api.requests)
	assert.Equal(t, 1, countRequests(api.requests, http.MethodPatch, "/variations/default"))
	assert.Equal(t, "Local", api.variation.Name)
}

// A variation with an explicit LaunchDarkly rule is never pushed,
// and the user learns why: on stderr, and as a notes entry in --output json.
func TestPushSkipsAVariationManagedInLaunchDarkly(t *testing.T) {
	for _, arguments := range [][]string{{"--yes"}, {"--yes", "--output", "json"}} {
		t.Run(strings.Join(arguments, " "), func(t *testing.T) {
			root, api := managedVariationWithLocalChange(t)
			managedInLaunchDarkly(api)

			stdout, stderr, err := runPrompt(t, root, api, arguments...)

			require.NoError(t, err)
			assert.Contains(t, stderr, managedMessage)
			if len(arguments) > 1 {
				assert.Contains(t, jsonNotes(t, stdout), managedMessage, "stdout: %s", stdout)
			}
			assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no push")
			assert.Equal(t, "Baseline", api.variation.Name)
		})
	}
}

// jsonNotes returns the notes of every JSON document in stdout.
func jsonNotes(t *testing.T, stdout string) []string {
	t.Helper()
	var notes []string
	decoder := json.NewDecoder(strings.NewReader(stdout))
	for {
		var document struct {
			Notes []string `json:"notes"`
		}
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return notes
		}
		require.NoError(t, err, "stdout is JSON: %s", stdout)
		notes = append(notes, document.Notes...)
	}
}

// A variation with an explicit LaunchDarkly rule still pulls the changes made
// in LaunchDarkly.
func TestPullOfAVariationManagedInLaunchDarkly(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(variation("Server"))}
	managedInLaunchDarkly(api)

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	compiled, err := synclocal.Compile(os.DirFS(root))
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	assert.Equal(t, "Server", compiled[0].Variation.Name)
	requireNoSettingsWrite(t, api.requests)
}

// D2: linked variations follow ownership too. A server_canonical direction
// that no rule produced is not a LaunchDarkly owner, so a linked variation is
// pushed.
func TestPushOfALinkedVariationWithoutARule(t *testing.T) {
	root, api := linkedVariationForPush(t)
	api.directions[supportDefaultSetting] = syncdomain.SyncDirectionServerCanonical

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	requireNoSettingsWrite(t, api.requests)
	assert.Equal(t, "Local text", api.variation.Instructions)
}

// A read of the sync settings that fails with an error other than 404 stops
// the push before any variation write.
func TestPushStopsWhenTheSyncSettingsReadFails(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.settingsReadErr = errors.New(`{"code":"internal","statusCode":500}`)

	_, _, err := runPrompt(t, root, api, "--yes")

	require.Error(t, err)
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no variation write")
	assert.Equal(t, "Baseline", api.variation.Name)
}

// A sync settings read that answers without an entry for a variation (here
// the 400 for an unsupported kind) fails closed: no push.
func TestPushFailsClosedWhenTheSyncSettingIsMissing(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.settingsReadErr = errors.New(`{"code":"invalid_request","message":"unsupported resource kind","statusCode":400}`)

	_, _, err := runPrompt(t, root, api, "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "LaunchDarkly returned no sync setting")
	assert.Contains(t, err.Error(), string(syncdomain.ErrorSyncSettingUnavailable))
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no push")
	assert.Equal(t, "Baseline", api.variation.Name)
}

// Without the sync settings API, there is no ownership to read, so the push
// goes ahead as before, with no warning.
func TestPushWithoutTheSettingsAPIPushes(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.settingsReadErr = errors.New(`{"code":"not_found","message":"Not found","statusCode":404}`)

	_, notes, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts, Action: syncprompt.SyncAction{}, Yes: true,
	})

	require.NoError(t, err)
	assert.NotContains(t, notes, "ownership")
	assert.Equal(t, 1, countRequests(api.requests, http.MethodPatch, "/variations/default"))
	assert.Equal(t, "Local", api.variation.Name)
}

// A push of a code-owned variation that the server lock rejects because the
// role lacks syncAIConfigVariation names that action, not a generic error.
func TestPromptPushNamesSyncAIConfigVariationWhenTheVariationWriteIsForbidden(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.directions = map[string]syncdomain.SyncDirection{supportDefaultSetting: syncdomain.SyncDirectionCodeCanonical}
	api.patterns = map[string]string{supportDefaultSetting: "support/default"}
	api.variationWriteErr = errors.New(`{"code":"forbidden","statusCode":403}`)

	_, _, err := runPrompt(t, root, api, "--yes", "--output=plaintext")

	require.Error(t, err)
	assert.Equal(t, 1, countRequests(api.requests, http.MethodPatch, "/ai-configs/support/variations/default"), "the variation write is tried")
	requireNoSettingsWrite(t, api.requests)
	assert.Contains(t, err.Error(), "syncAIConfigVariation")
	assert.Contains(t, err.Error(), "proj/production:env/*:aiconfig/support", "names the AI Config resource as roles use it")
}

// A link of a LaunchDarkly variation that no rule matches pushes it and
// writes no rule.
func TestLinkOfAVariationWithoutARule(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local prompt.\n"), 0o644))
	api := &directAPI{variation: pointer(variation("Server"))}

	_, err := linkPrompt(t, root, api)

	require.NoError(t, err)
	requireNoSettingsWrite(t, api.requests)
	assert.Equal(t, "Local prompt.", api.variation.Instructions)
}

// A link of a variation with an explicit LaunchDarkly rule does not push it,
// and the user learns why.
func TestLinkLeavesAVariationManagedInLaunchDarkly(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local prompt.\n"), 0o644))
	api := &directAPI{variation: pointer(variation("Server"))}
	managedInLaunchDarkly(api)

	notes, err := linkPrompt(t, root, api)

	require.NoError(t, err)
	assert.Contains(t, notes, "support/default is managed in LaunchDarkly")
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no push")
	assert.Equal(t, "Help", api.variation.Instructions)
}

// A 403 that the server lock sends because code owns the variation is not a
// missing permission: the error passes on what LaunchDarkly said instead of
// asking for syncAIConfigVariation.
func TestPromptPushDoesNotBlameTheRoleWhenTheLockRejectsTheWrite(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.directions = map[string]syncdomain.SyncDirection{supportDefaultSetting: syncdomain.SyncDirectionCodeCanonical}
	api.patterns = map[string]string{supportDefaultSetting: "support/default"}
	api.variationWriteErr = errors.New(`{"code":"managed_in_code","message":"Variation default in AI Config support is managed in code, so LaunchDarkly rejected this change.","statusCode":403}`)

	_, _, err := runPrompt(t, root, api, "--yes", "--output=plaintext")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "syncAIConfigVariation", "the caller holds the permission; the lock is the cause")
	assert.Contains(t, err.Error(), "is managed in code")
}

// A create that an explicit LaunchDarkly rule blocks is an error, not a pull,
// so it gets no "pulled, not pushed" note.
func TestCreateBlockedByALaunchDarklyRuleHasNoManagedNote(t *testing.T) {
	root := initRepository(t)
	writeVariation(t, root, variation("New"), true)
	api := &directAPI{}
	api.directions = map[string]syncdomain.SyncDirection{supportDefaultSetting: syncdomain.SyncDirectionServerCanonical}
	api.patterns = map[string]string{supportDefaultSetting: "support/*"}

	_, stderr, err := runPrompt(t, root, api, "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server-canonical resource does not exist in LaunchDarkly")
	assert.NotContains(t, stderr, "pulled, not pushed")
}

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

// settingsWrite is the body of a write of the sync settings.
type settingsWrite struct {
	Upserts []struct {
		ResourceKind    string `json:"resourceKind"`
		ResourcePattern string `json:"resourcePattern"`
		Direction       string `json:"direction"`
	} `json:"upserts"`
	Deletions []any `json:"deletions"`
}

// codeClaims returns "kind:pattern direction" for each upserted rule.
func codeClaims(writes []settingsWrite) []string {
	claims := []string{}
	for _, write := range writes {
		for _, rule := range write.Upserts {
			claims = append(claims, rule.ResourceKind+":"+rule.ResourcePattern+" "+rule.Direction)
		}
	}
	return claims
}

// requestIndex returns the index of the first request with the method and
// the path suffix, or -1.
func requestIndex(requests []string, method, pathSuffix string) int {
	for index, request := range requests {
		if strings.HasPrefix(request, method+" ") && strings.HasSuffix(request, pathSuffix) {
			return index
		}
	}
	return -1
}

// requireClaimBeforeWrite checks that the claim of the sync settings and the
// variation write were both sent, the claim first.
func requireClaimBeforeWrite(t *testing.T, requests []string) {
	t.Helper()
	claim := requestIndex(requests, http.MethodPatch, "/configs/sync/settings")
	write := requestIndex(requests, http.MethodPatch, "/variations/default")
	require.NotEqual(t, -1, claim, "the claim is sent")
	require.NotEqual(t, -1, write, "the variation is written")
	assert.Less(t, claim, write, "the claim comes before the variation write")
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

// A push of a variation that no rule matches claims it for code before it
// writes the variation.
func TestPushClaimsAVariationWithoutARuleBeforeWritingIt(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, []string{"variation:support/default code_canonical"}, codeClaims(api.settingsWrites))
	assert.Equal(t, 1, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"), "one claim per project")
	requireClaimBeforeWrite(t, api.requests)
	assert.Equal(t, "Local", api.variation.Name)
}

// A variation with an exact code rule is already owned by code, so the push
// does not claim it again.
func TestPushDoesNotClaimAVariationWithAnExactCodeRule(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.directions = map[string]syncdomain.SyncDirection{supportDefaultSetting: syncdomain.SyncDirectionCodeCanonical}
	api.patterns = map[string]string{supportDefaultSetting: "support/default"}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"))
	assert.Equal(t, 1, countRequests(api.requests, http.MethodPatch, "/variations/default"))
	assert.Equal(t, "Local", api.variation.Name)
}

// A variation with an explicit LaunchDarkly rule is never pushed or claimed,
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
			assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"), "no claim")
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
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"))
}

// D2: linked variations follow ownership too. A server_canonical direction
// that no rule produced is not a LaunchDarkly owner, so a linked variation is
// claimed and pushed.
func TestPushClaimsALinkedVariationWithoutARule(t *testing.T) {
	root, api := linkedVariationForPush(t)
	api.directions[supportDefaultSetting] = syncdomain.SyncDirectionServerCanonical

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Equal(t, []string{"variation:support/default code_canonical"}, codeClaims(api.settingsWrites))
	requireClaimBeforeWrite(t, api.requests)
	assert.Equal(t, "Local text", api.variation.Instructions)
}

// A role that cannot write the sync settings stops the push before any
// variation write, with a message that names the missing permission.
func TestPushStopsWhenTheClaimIsForbidden(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.settingsWriteErr = errors.New(`{"code":"forbidden","statusCode":403}`)

	_, _, err := runPrompt(t, root, api, "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "code ownership")
	assert.Contains(t, err.Error(), "updateSyncSettings")
	assert.Contains(t, err.Error(), "proj/production:ai-code-sync/*")
	assert.Equal(t, 1, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"), "the claim is tried")
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no variation write")
	assert.Equal(t, "Baseline", api.variation.Name)
}

// A read or a claim of the sync settings that fails with an error other than
// 404 or 403 stops the push before any variation write.
func TestPushStopsWhenTheSyncSettingsFail(t *testing.T) {
	internal := errors.New(`{"code":"internal","statusCode":500}`)
	for name, configure := range map[string]func(*directAPI){
		"read":  func(api *directAPI) { api.settingsReadErr = internal },
		"claim": func(api *directAPI) { api.settingsWriteErr = internal },
	} {
		t.Run(name, func(t *testing.T) {
			root, api := managedVariationWithLocalChange(t)
			configure(api)

			_, _, err := runPrompt(t, root, api, "--yes")

			require.Error(t, err)
			assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no variation write")
			assert.Equal(t, "Baseline", api.variation.Name)
		})
	}
}

// A sync settings read that answers without an entry for a variation (here
// the 400 for an unsupported kind) fails closed: no claim and no push.
func TestPushFailsClosedWhenTheSyncSettingIsMissing(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.settingsReadErr = errors.New(`{"code":"invalid_request","message":"unsupported resource kind","statusCode":400}`)

	_, _, err := runPrompt(t, root, api, "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "LaunchDarkly returned no sync setting")
	assert.Contains(t, err.Error(), string(syncdomain.ErrorSyncSettingUnavailable))
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"), "no claim")
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no push")
	assert.Equal(t, "Baseline", api.variation.Name)
}

// Without the sync settings API, the push warns, naming the project, that
// ownership is not recorded. --yes proceeds, whether the read or the claim
// after the confirmation finds the API missing.
func TestPushWithoutTheSettingsAPIProceedsWithYes(t *testing.T) {
	notFound := errors.New(`{"code":"not_found","message":"Not found","statusCode":404}`)
	for name, configure := range map[string]func(*directAPI){
		"read":  func(api *directAPI) { api.settingsReadErr = notFound },
		"claim": func(api *directAPI) { api.settingsWriteErr = notFound },
	} {
		t.Run(name, func(t *testing.T) {
			root, api := managedVariationWithLocalChange(t)
			configure(api)

			_, notes, err := runScoped(t, root, api, syncprompt.Options{
				Scope: syncdomain.ScopePrompts, Action: syncprompt.SyncAction{}, Yes: true,
			})

			require.NoError(t, err)
			assert.Contains(t, notes, "ownership")
			assert.Contains(t, notes, "in project production", "the warning names the project")
			assert.Equal(t, 1, countRequests(api.requests, http.MethodPatch, "/variations/default"))
			assert.Equal(t, "Local", api.variation.Name)
		})
	}
}

// Without the sync settings API and without a terminal, the push needs --yes
// and writes no variation.
func TestPushWithoutTheSettingsAPINeedsYes(t *testing.T) {
	root, api := managedVariationWithLocalChange(t)
	api.settingsReadErr = errors.New(`{"code":"not_found","message":"Not found","statusCode":404}`)

	_, notes, err := runScoped(t, root, api, syncprompt.Options{
		Scope: syncdomain.ScopePrompts, Action: syncprompt.SyncAction{},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--yes")
	assert.Contains(t, notes, "ownership")
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no variation write")
	assert.Equal(t, "Baseline", api.variation.Name)
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
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"), "no claim is needed")
	assert.Contains(t, err.Error(), "syncAIConfigVariation")
	assert.Contains(t, err.Error(), "proj/production:env/*:aiconfig/support", "names the AI Config resource as roles use it")
}

// A link of a LaunchDarkly variation that no rule matches makes it code-owned.
func TestLinkClaimsAVariationWithoutARule(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local prompt.\n"), 0o644))
	api := &directAPI{variation: pointer(variation("Server"))}

	_, err := linkPrompt(t, root, api)

	require.NoError(t, err)
	assert.Equal(t, []string{"variation:support/default code_canonical"}, codeClaims(api.settingsWrites))
	requireClaimBeforeWrite(t, api.requests)
	assert.Equal(t, "Local prompt.", api.variation.Instructions)
}

// A link makes the variation code-owned even when its content already matches
// LaunchDarkly, so nothing else is pushed.
func TestLinkClaimsAVariationWhoseContentAlreadyMatches(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Help\n"), 0o644))
	api := &directAPI{variation: pointer(variation("Server"))}

	_, err := linkPrompt(t, root, api)

	require.NoError(t, err)
	assert.Equal(t, []string{"variation:support/default code_canonical"}, codeClaims(api.settingsWrites))
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no content push")
}

// A link of a variation with an explicit LaunchDarkly rule neither claims nor
// pushes it, and the user learns why.
func TestLinkLeavesAVariationManagedInLaunchDarkly(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local prompt.\n"), 0o644))
	api := &directAPI{variation: pointer(variation("Server"))}
	managedInLaunchDarkly(api)

	notes, err := linkPrompt(t, root, api)

	require.NoError(t, err)
	assert.Contains(t, notes, "support/default is managed in LaunchDarkly")
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"), "no claim")
	assert.Zero(t, countRequests(api.requests, http.MethodPatch, "/variations/default"), "no push")
	assert.Equal(t, "Help", api.variation.Instructions)
}

// A role that cannot write the sync settings stops a detach with a message
// that names the missing permission, not with "unknown error occurred", and
// the variation stays tracked.
func TestDetachNamesTheMissingPermissionWhenReturningToLaunchDarklyIsForbidden(t *testing.T) {
	root := initRepository(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Local prompt.\n"), 0o644))
	api := &directAPI{variation: pointer(variation("Server"))}
	_, err := linkPrompt(t, root, api)
	require.NoError(t, err)
	descriptor := filepath.Join(root, ".launchdarkly", "production", "configs", "support", "default.ref.yaml")
	require.FileExists(t, descriptor)

	api.settingsWriteErr = errors.New(`{"code":"forbidden","statusCode":403}`)
	before := countRequests(api.requests, http.MethodPatch, "/configs/sync/settings")
	_, _, err = runPrompt(t, root, api, "detach", "--project", "production", "--config-key", "support", "--key", "default", "--yes", "--no-input", "--output=plaintext")

	require.Error(t, err)
	assert.Equal(t, before+1, countRequests(api.requests, http.MethodPatch, "/configs/sync/settings"), "the return is tried")
	assert.Contains(t, err.Error(), "updateSyncSettings")
	assert.Contains(t, err.Error(), "proj/production:ai-code-sync/*")
	assert.NotContains(t, err.Error(), "unknown error occurred")
	assert.FileExists(t, descriptor, "nothing is detached")
}

package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

var _ Settings = syncapi.Client{}

// recordingSettings stands in for the LaunchDarkly sync settings API. It
// answers ResolveOwnership from ownership, leaves out the resources in
// omitted, fails the reads and claims of the projects in resolveErrs and
// claimErrs, records every claim, and records
// whether a claimed variation's file was already on disk when it was claimed.
type recordingSettings struct {
	root        string
	ownership   map[syncdomain.ResourceID]syncdomain.Ownership
	resolveErrs map[string]error
	claimErrs   map[string]error
	omitted     []syncdomain.ResourceID

	claims []claim
}

type claim struct {
	projectKey string
	ids        []syncdomain.ResourceID
	// writtenBefore names each claimed variation whose file existed when the
	// claim was made.
	writtenBefore []syncdomain.ResourceID
}

func (settings *recordingSettings) ResolveOwnership(
	projectKey string, ids []syncdomain.ResourceID,
) (map[syncdomain.ResourceID]syncdomain.Ownership, error) {
	if err := settings.resolveErrs[projectKey]; err != nil {
		return nil, err
	}
	result := make(map[syncdomain.ResourceID]syncdomain.Ownership, len(ids))
	for _, id := range ids {
		if id.ProjectKey != projectKey {
			return nil, fmt.Errorf("%s is not in project %q", id.LookupKey, projectKey)
		}
		if slices.Contains(settings.omitted, id) {
			continue
		}
		// A variation without a stored rule resolves to the project default
		// with no matched pattern.
		owner, ok := settings.ownership[id]
		if !ok {
			owner = syncdomain.Ownership{Direction: syncdomain.SyncDirectionServerCanonical}
		}
		result[id] = owner
	}
	return result, nil
}

func (settings *recordingSettings) ClaimForCode(projectKey string, ids []syncdomain.ResourceID) error {
	recorded := claim{projectKey: projectKey, ids: slices.Clone(ids)}
	for _, id := range ids {
		if _, err := os.Stat(variationPath(settings.root, id)); err == nil {
			recorded.writtenBefore = append(recorded.writtenBefore, id)
		}
	}
	settings.claims = append(settings.claims, recorded)
	return settings.claimErrs[projectKey]
}

// claimedIDs returns every claimed variation, in claim order.
func (settings *recordingSettings) claimedIDs() []syncdomain.ResourceID {
	var ids []syncdomain.ResourceID
	for _, claim := range settings.claims {
		ids = append(ids, claim.ids...)
	}
	return ids
}

func variationPath(root string, id syncdomain.ResourceID) string {
	configKey, variationKey, err := id.VariationKeys()
	if err != nil {
		panic(err)
	}
	return filepath.Join(root, syncdomain.RootDir, id.ProjectKey, "configs", configKey, variationKey+".prompt.md")
}

func ownershipCatalog(variationKeys ...string) *fakeCatalog {
	config := syncapi.Config{Key: "config", Mode: syncdomain.VariationModeAgent}
	for _, key := range variationKeys {
		config.Variations = append(config.Variations, syncdomain.Variation{
			Key: key, Name: key, Mode: syncdomain.VariationModeAgent, Instructions: "Be helpful.",
		})
	}
	return &fakeCatalog{config: config}
}

type ownershipRun struct {
	root      string
	settings  *recordingSettings
	baselines *memoryManifestStore
	output    bytes.Buffer
	errOutput bytes.Buffer
}

func newOwnershipRun(t *testing.T) *ownershipRun {
	root := t.TempDir()
	return &ownershipRun{
		root: root,
		settings: &recordingSettings{
			root:        root,
			ownership:   map[syncdomain.ResourceID]syncdomain.Ownership{},
			resolveErrs: map[string]error{},
			claimErrs:   map[string]error{},
		},
		baselines: &memoryManifestStore{manifest: syncmanifest.New()},
	}
}

func (run *ownershipRun) options(catalog Catalog, selections ...syncdomain.ResourceID) Options {
	return Options{
		Catalog:     catalog,
		Store:       synclocal.NewStore(run.root),
		Baselines:   run.baselines,
		Settings:    run.settings,
		Input:       bytes.NewBuffer(nil),
		Output:      &run.output,
		ErrorOutput: &run.errOutput,
		Initial:     true,
		Selections:  selections,
	}
}

// singleAdd adds the new variation project/config/variation.
func singleAdd(t *testing.T) (*ownershipRun, syncdomain.ResourceID, Options) {
	run := newOwnershipRun(t)
	id := syncdomain.VariationID("project", "config", "variation")
	return run, id, run.options(ownershipCatalog("variation"), id)
}

// multiProjectAdd adds two new variations in project alpha and one in beta.
func multiProjectAdd(t *testing.T) (*ownershipRun, Options) {
	run := newOwnershipRun(t)
	options := run.options(ownershipCatalog("first", "second"),
		syncdomain.VariationID("alpha", "config", "first"),
		syncdomain.VariationID("alpha", "config", "second"),
		syncdomain.VariationID("beta", "config", "first"),
	)
	return run, options
}

func (run *ownershipRun) assertWritten(t *testing.T, id syncdomain.ResourceID) {
	t.Helper()
	_, err := os.Stat(variationPath(run.root, id))
	assert.NoError(t, err, "the file of %s was not written", id.LookupKey)
}

func (run *ownershipRun) assertNothingWritten(t *testing.T) {
	t.Helper()
	_, err := os.Stat(filepath.Join(run.root, syncdomain.RootDir))
	assert.ErrorIs(t, err, os.ErrNotExist, "the add wrote files")
	assert.Empty(t, run.baselines.manifest.Resources, "the add recorded a lock entry")
}

func settingsNotFound() error {
	return errors.New(`{"code":"not_found","statusCode":404}`)
}

func settingsForbidden() error {
	return fmt.Errorf("%w: your role needs the updateSyncSettings action", syncapi.ErrSyncSettingsForbidden)
}

// lineWith returns the first line of text that contains every word.
func lineWith(text string, words ...string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		found := true
		for _, word := range words {
			found = found && strings.Contains(line, word)
		}
		if found {
			return line, true
		}
	}
	return "", false
}

func TestRunClaimsAVariationWithoutARuleBeforeWritingIt(t *testing.T) {
	run, id, options := singleAdd(t)

	require.NoError(t, Run(options))

	require.Len(t, run.settings.claims, 1)
	assert.Equal(t, "project", run.settings.claims[0].projectKey)
	assert.Equal(t, []syncdomain.ResourceID{id}, run.settings.claims[0].ids)
	assert.Empty(t, run.settings.claims[0].writtenBefore, "the claim must come before the files are written")
	run.assertWritten(t, id)
	assert.True(t, run.baselines.manifest.Has(id))
}

func TestRunPullsButDoesNotClaimAVariationManagedInLaunchDarkly(t *testing.T) {
	run := newOwnershipRun(t)
	managed := syncdomain.VariationID("project", "config", "managed")
	unowned := syncdomain.VariationID("project", "config", "unowned")
	run.settings.ownership[managed] = syncdomain.Ownership{
		Direction: syncdomain.SyncDirectionServerCanonical, MatchedPattern: "config/managed",
	}

	err := Run(run.options(ownershipCatalog("managed", "unowned"), managed, unowned))

	require.NoError(t, err)
	assert.Equal(t, []syncdomain.ResourceID{unowned}, run.settings.claimedIDs())
	run.assertWritten(t, managed)
	run.assertWritten(t, unowned)
	assert.Contains(t, run.errOutput.String(),
		"config/managed is managed in LaunchDarkly; pulled, not pushed. "+
			"To manage it in code, switch it to Code in LaunchDarkly.")
	assert.NotContains(t, run.errOutput.String(), "config/unowned is managed in LaunchDarkly")
}

func TestRunClaimsOncePerProject(t *testing.T) {
	run, options := multiProjectAdd(t)

	require.NoError(t, Run(options))

	require.Len(t, run.settings.claims, 2, "one claim per project")
	byProject := map[string][]syncdomain.ResourceID{}
	for _, claim := range run.settings.claims {
		require.NotContains(t, byProject, claim.projectKey, "project %s was claimed twice", claim.projectKey)
		byProject[claim.projectKey] = claim.ids
		assert.Empty(t, claim.writtenBefore, "the claim must come before the files are written")
	}
	assert.ElementsMatch(t, []syncdomain.ResourceID{
		syncdomain.VariationID("alpha", "config", "first"), syncdomain.VariationID("alpha", "config", "second"),
	}, byProject["alpha"])
	assert.Equal(t, []syncdomain.ResourceID{syncdomain.VariationID("beta", "config", "first")}, byProject["beta"])
}

// Only a variation's first entry in the repo is claimed. The lock tracks
// this one, but its file is gone, so the add writes it again.
func TestRunDoesNotClaimAVariationAlreadyTrackedInTheLock(t *testing.T) {
	run := newOwnershipRun(t)
	tracked := syncdomain.VariationID("project", "config", "tracked")
	fresh := syncdomain.VariationID("project", "config", "fresh")
	run.baselines.manifest.SetFingerprint(tracked, "previous")

	err := Run(run.options(ownershipCatalog("tracked", "fresh"), tracked, fresh))

	require.NoError(t, err)
	assert.Equal(t, []syncdomain.ResourceID{fresh}, run.settings.claimedIDs())
}

func TestRunDryRunDoesNotClaim(t *testing.T) {
	run, _, options := singleAdd(t)
	options.DryRun = true

	require.NoError(t, Run(options))

	assert.Empty(t, run.settings.claims, "a dry run must not change the sync settings")
	run.assertNothingWritten(t)
}

// The CLI renders a sync problem with its message and suggestion. A forbidden
// claim must reach it as one, or the user sees "unknown error occurred".
func TestRunWritesNothingWhenTheClaimIsForbidden(t *testing.T) {
	run, _, options := singleAdd(t)
	run.settings.claimErrs["project"] = settingsForbidden()

	err := Run(options)

	require.ErrorIs(t, err, syncapi.ErrSyncSettingsForbidden)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok, "want a sync problem, got %v", err)
	assert.Equal(t, syncdomain.ErrorSyncSettingsForbidden, problem.Code)
	assert.Contains(t, problem.Error(), "code ownership")
	assert.Contains(t, problem.Error(), "updateSyncSettings")
	assert.Contains(t, problem.Error(), "proj/project:ai-code-sync/*")
	require.Len(t, run.settings.claims, 1)
	run.assertNothingWritten(t)
}

// A claim that fails with an error other than 404 or 403 stops the add
// before anything is written.
func TestRunWritesNothingWhenTheSettingsFail(t *testing.T) {
	run, _, options := singleAdd(t)
	run.settings.claimErrs["project"] = errors.New(`{"code":"internal","statusCode":500}`)

	err := Run(options)

	require.ErrorContains(t, err, "statusCode\":500")
	run.assertNothingWritten(t)
}

// A sync settings read that answers without an entry for a new variation
// fails closed: nothing is claimed or written.
func TestRunFailsClosedWhenTheSyncSettingIsMissing(t *testing.T) {
	run, id, options := singleAdd(t)
	run.settings.omitted = []syncdomain.ResourceID{id}
	options.Yes = true

	err := Run(options)

	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok, "want a sync problem, got %v", err)
	assert.Equal(t, syncdomain.ErrorSyncSettingUnavailable, problem.Code)
	assert.Equal(t, "LaunchDarkly returned no sync setting", problem.Message)
	assert.Empty(t, run.settings.claims)
	run.assertNothingWritten(t)
}

func TestRunWarnsAndContinuesWithYesWhenSettingsAreUnavailable(t *testing.T) {
	run, id, options := singleAdd(t)
	run.settings.resolveErrs["project"] = settingsNotFound()
	options.Yes = true

	require.NoError(t, Run(options))

	assert.Contains(t, run.errOutput.String(), "ownership")
	run.assertWritten(t, id)
	assert.True(t, run.baselines.manifest.Has(id))
}

func TestRunRequiresYesWithoutATerminalWhenSettingsAreUnavailable(t *testing.T) {
	run, _, options := singleAdd(t)
	run.settings.resolveErrs["project"] = settingsNotFound()

	err := Run(options)

	require.ErrorContains(t, err, "--yes")
	run.assertNothingWritten(t)
}

// On a terminal, a 404 from the sync settings warns on the error output and
// asks, on the output that the variation picker uses, whether to add without
// recording ownership. A yes adds. (A no: see the cancel test below.)
func TestRunAsksOnATerminalWhenSettingsAreUnavailable(t *testing.T) {
	run, id, options := singleAdd(t)
	run.settings.resolveErrs["project"] = settingsNotFound()
	// One byte per read, so the question reads only its own line.
	options.Input = iotest.OneByteReader(strings.NewReader("y\n"))
	var checked []io.Writer
	options.isTerminal = func(_ io.Reader, writer io.Writer) bool {
		checked = append(checked, writer)
		return true
	}

	require.NoError(t, Run(options))

	assert.Contains(t, run.output.String(), "Add without recording ownership? [y/N]")
	assert.NotContains(t, run.errOutput.String(), "[y/N]")
	assert.Contains(t, run.errOutput.String(), "ownership", "the warning stays on the error output")
	require.NotEmpty(t, checked)
	for _, writer := range checked {
		assert.True(t, writer == io.Writer(&run.output), "the terminal check uses the output, not the error output")
	}
	run.assertWritten(t, id)
	assert.True(t, run.baselines.manifest.Has(id))
}

// The settings-unavailable question honors the context of the command. With
// a context that has already ended, Run does not wait for an answer and adds
// nothing, even though the input would answer yes.
func TestRunStopsTheSettingsQuestionWhenTheContextEnded(t *testing.T) {
	run, _, options := singleAdd(t)
	run.settings.resolveErrs["project"] = settingsNotFound()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options.Context = ctx
	options.Input = strings.NewReader("y\n")
	options.isTerminal = func(io.Reader, io.Writer) bool { return true }

	done := make(chan error, 1)
	go func() { done <- Run(options) }()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Run waited for input after the context ended")
	}
	assert.NotContains(t, run.output.String()+run.errOutput.String(), "[y/N]", "no question is asked")
	run.assertNothingWritten(t)
}

// A 404 in one project must not leave another project claimed when the user
// then cancels the add, so every project is read before any claim.
func TestRunDoesNotClaimAnyProjectWhenTheUserCancelsAfterA404(t *testing.T) {
	run, options := multiProjectAdd(t)
	run.settings.resolveErrs["beta"] = settingsNotFound()
	options.Input = iotest.OneByteReader(strings.NewReader("n\n"))
	options.isTerminal = func(io.Reader, io.Writer) bool { return true }

	require.NoError(t, Run(options))

	assert.Empty(t, run.settings.claims, "a canceled add must not claim any project")
	assert.Contains(t, run.output.String(), "Add canceled.")
	_, named := lineWith(run.errOutput.String(), "beta")
	assert.True(t, named, "the settings-unavailable warning names beta: %q", run.errOutput.String())
	run.assertNothingWritten(t)
}

// A generic read failure in one project stops the add before any claim, even
// when another project was already read.
func TestRunDoesNotClaimAnyProjectWhenAReadFails(t *testing.T) {
	run, options := multiProjectAdd(t)
	run.settings.resolveErrs["beta"] = errors.New(`{"code":"internal","statusCode":500}`)

	err := Run(options)

	require.ErrorContains(t, err, "statusCode\":500")
	assert.Empty(t, run.settings.claims, "a failed read must not leave a claim")
	run.assertNothingWritten(t)
}

// A 403 on the claim of the second project comes after the first project was
// claimed. The error says so, instead of saying that nothing changed.
func TestRunForbiddenClaimInTheSecondProjectSaysTheFirstWasClaimed(t *testing.T) {
	run, options := multiProjectAdd(t)
	run.settings.claimErrs["beta"] = settingsForbidden()

	err := Run(options)

	require.ErrorIs(t, err, syncapi.ErrSyncSettingsForbidden)
	require.Len(t, run.settings.claims, 2)
	_, says := lineWith(err.Error(), "alpha", "already")
	assert.True(t, says, "the error says alpha was already claimed for code: %q", err.Error())
	assert.NotContains(t, err.Error(), "nothing changed")
	run.assertNothingWritten(t)
}

// The claim comes before the files and the baseline. When a later step
// fails, the error says that the variations were claimed for code, so the
// user knows that the add can run again. The check hides the temporary
// directory, because its path carries the test name.
func TestRunSaysTheVariationsAreOwnedByCodeWhenALaterStepFails(t *testing.T) {
	run, id, options := singleAdd(t)
	// Store.Bootstrap refuses to replace an existing managed directory.
	require.NoError(t, os.Mkdir(filepath.Join(run.root, syncdomain.RootDir), 0o755))

	err := Run(options)

	require.Error(t, err)
	require.Equal(t, []syncdomain.ResourceID{id}, run.settings.claimedIDs(), "the claim ran before the failure")
	message := strings.ReplaceAll(err.Error(), run.root, "<root>")
	assert.Contains(t, message, "claimed", "the error says the variations were claimed for code: %q", message)
	_, statErr := os.Stat(variationPath(run.root, id))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the add kept the variation file")
}

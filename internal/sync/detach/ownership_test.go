package detach

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncmanifest "github.com/launchdarkly/ldcli/internal/sync/manifest"
)

// recordingSettings stands in for the LaunchDarkly sync settings API. Every
// variation resolves to the project default with no matched rule. It fails
// the reads and writes of the projects in resolveErrs and returnErrs, and
// logs every read and write in order. When workspace is set, each write
// records whether every variation it names still has its local file and
// lock entry.
type recordingSettings struct {
	workspace   *archiveWorkspace
	resolveErrs map[string]error
	returnErrs  map[string]error
	log         []string
	calls       []settingsCall
}

type settingsCall struct {
	projectKey    string
	ids           []syncdomain.ResourceID
	trackedAtCall []bool
}

func newRecordingSettings() *recordingSettings {
	return &recordingSettings{resolveErrs: map[string]error{}, returnErrs: map[string]error{}}
}

func (settings *recordingSettings) ResolveOwnership(
	projectKey string, ids []syncdomain.ResourceID,
) (map[syncdomain.ResourceID]syncdomain.Ownership, error) {
	settings.log = append(settings.log, "resolve "+projectKey)
	if err := settings.resolveErrs[projectKey]; err != nil {
		return nil, err
	}
	result := make(map[syncdomain.ResourceID]syncdomain.Ownership, len(ids))
	for _, id := range ids {
		if id.ProjectKey != projectKey {
			return nil, fmt.Errorf("%s is not in project %q", id.Label(), projectKey)
		}
		result[id] = syncdomain.Ownership{Direction: syncdomain.SyncDirectionServerCanonical}
	}
	return result, nil
}

func (settings *recordingSettings) ReturnToLaunchDarkly(projectKey string, ids []syncdomain.ResourceID) error {
	settings.log = append(settings.log, "return "+projectKey)
	call := settingsCall{projectKey: projectKey, ids: append([]syncdomain.ResourceID(nil), ids...)}
	if settings.workspace != nil {
		for _, id := range ids {
			configKey, variationKey, err := id.VariationKeys()
			if err != nil {
				return err
			}
			exists, err := settings.workspace.store.VariationExists(id.ProjectKey, configKey, variationKey)
			if err != nil {
				return err
			}
			call.trackedAtCall = append(call.trackedAtCall, exists && settings.workspace.baselines.manifest.Has(id))
		}
	}
	settings.calls = append(settings.calls, call)
	return settings.returnErrs[projectKey]
}

func (settings *recordingSettings) returnedProjects() []string {
	projects := make([]string, 0, len(settings.calls))
	for _, call := range settings.calls {
		projects = append(projects, call.projectKey)
	}
	return projects
}

func notFound() error {
	return errors.New(`{"code":"not_found","statusCode":404}`)
}

func forbidden() error {
	return fmt.Errorf("%w: your role needs the updateSyncSettings action", syncapi.ErrSyncSettingsForbidden)
}

// singleDetach is a workspace with the synced variation project/config/prompt,
// selected for the detach.
func singleDetach(t *testing.T) (archiveWorkspace, *recordingSettings, Options) {
	t.Helper()
	workspace := newArchiveWorkspace(t, "prompt")
	settings := newRecordingSettings()
	options := workspace.options(&recordingArchiver{})
	options.Settings, options.ErrorOutput = settings, &bytes.Buffer{}
	return workspace, settings, options
}

// multiProjectDetach is a workspace with one synced variation in project
// alpha and one in beta, both selected for the detach.
type multiProjectDetach struct {
	workspace  archiveWorkspace
	settings   *recordingSettings
	ids        []syncdomain.ResourceID
	lockBefore syncmanifest.Manifest
	output     bytes.Buffer
	errOutput  bytes.Buffer
}

func newMultiProjectDetach(t *testing.T) (*multiProjectDetach, Options) {
	t.Helper()
	detach := &multiProjectDetach{workspace: newArchiveWorkspace(t), settings: newRecordingSettings()}
	for _, projectKey := range []string{"alpha", "beta"} {
		_, err := detach.workspace.store.Add([]synclocal.VariationFile{{
			ProjectKey: projectKey, ConfigKey: "config", Variation: testVariation("prompt"),
		}})
		require.NoError(t, err)
		id := syncdomain.VariationID(projectKey, "config", "prompt")
		detach.workspace.baselines.manifest.SetFingerprint(id, testFingerprint())
		detach.ids = append(detach.ids, id)
	}
	detach.lockBefore = detach.workspace.baselines.manifest.Clone()
	options := detach.workspace.options(&recordingArchiver{})
	options.Selections = detach.ids
	options.Settings = detach.settings
	options.Output, options.ErrorOutput = &detach.output, &detach.errOutput
	return detach, options
}

// interactive makes the output a terminal that answers each question with
// the next answer. One byte per read, so each question reads only its own
// line.
func interactive(options *Options, answers string) {
	options.NoInput = false
	options.Input = iotest.OneByteReader(strings.NewReader(answers))
	output := options.Output
	options.isTerminal = func(_ io.Reader, writer io.Writer) bool { return writer == output }
}

func (detach *multiProjectDetach) requireUnchanged(t *testing.T) {
	t.Helper()
	for _, id := range detach.ids {
		exists, err := detach.workspace.store.VariationExists(id.ProjectKey, "config", "prompt")
		require.NoError(t, err)
		assert.True(t, exists, "the file of %s was deleted", id)
	}
	assert.Equal(t, detach.lockBefore, detach.workspace.baselines.manifest)
}

func (detach *multiProjectDetach) requireDetached(t *testing.T) {
	t.Helper()
	for _, id := range detach.ids {
		exists, err := detach.workspace.store.VariationExists(id.ProjectKey, "config", "prompt")
		require.NoError(t, err)
		assert.False(t, exists, "the file of %s was kept", id)
		assert.False(t, detach.workspace.baselines.manifest.Has(id), "the lock entry of %s was kept", id)
	}
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

// Detach returns a variation to LaunchDarkly before it deletes anything, so
// a detach that fails after the write can run again.
func TestRunReturnsADetachedVariationToLaunchDarklyBeforeTheDelete(t *testing.T) {
	workspace, settings, options := singleDetach(t)
	settings.workspace = &workspace
	options.Yes = true

	require.NoError(t, Run(options))

	prompt := syncdomain.VariationID("project", "config", "prompt")
	require.Len(t, settings.calls, 1)
	assert.Equal(t, "project", settings.calls[0].projectKey)
	assert.Equal(t, []syncdomain.ResourceID{prompt}, settings.calls[0].ids)
	assert.Equal(t, []bool{true}, settings.calls[0].trackedAtCall, "the write runs before the local file and lock entry are removed")
	workspace.requireTracked(t, "prompt", false)
}

// Detach reads every project before the first write, so a read failure in
// any project leaves no rule behind, then writes once per project.
func TestRunReturnsVariationsToLaunchDarklyOncePerProject(t *testing.T) {
	detach, options := newMultiProjectDetach(t)
	options.Yes = true

	require.NoError(t, Run(options))

	assert.Equal(t, []string{"resolve alpha", "resolve beta", "return alpha", "return beta"}, detach.settings.log)
	require.Len(t, detach.settings.calls, 2)
	for index, call := range detach.settings.calls {
		assert.Equal(t, []syncdomain.ResourceID{detach.ids[index]}, call.ids)
	}
	detach.requireDetached(t)
}

// Only variations have sync settings rules. LaunchDarkly keeps a detached
// tool or skill without one.
func TestRunDoesNotWriteSyncSettingsForToolsAndSkills(t *testing.T) {
	workspace, settings, options := singleDetach(t)
	tool, skill := testTool(), testSkill()
	workspace.addAttachments(t, tool, skill)
	options.Selections = []syncdomain.ResourceID{tool.ID("project"), skill.ID("project")}
	options.Yes = true

	require.NoError(t, Run(options))

	assert.Empty(t, settings.calls)
}

// A 403 describes returning ownership to LaunchDarkly, not recording code
// ownership, and names the missing permission. Nothing is detached.
func TestRunKeepsEverythingWhenTheSyncSettingsWriteIsForbidden(t *testing.T) {
	workspace, settings, options := singleDetach(t)
	lockBefore := workspace.baselines.manifest.Clone()
	settings.returnErrs["project"] = forbidden()
	options.Yes = true

	err := Run(options)

	require.ErrorIs(t, err, syncapi.ErrSyncSettingsForbidden)
	problem, ok := syncdomain.ProblemFrom(err)
	require.True(t, ok, "a sync problem: %v", err)
	assert.Equal(t, syncdomain.ErrorSyncSettingsForbidden, problem.Code)
	assert.NotContains(t, problem.Message, "code ownership")
	assert.Contains(t, problem.Message, "LaunchDarkly", "the message names returning ownership to LaunchDarkly")
	assert.Contains(t, problem.Suggestion, "updateSyncSettings")
	assert.Contains(t, problem.Suggestion, "proj/project:ai-code-sync/*")
	require.Len(t, settings.calls, 1)
	workspace.requireTracked(t, "prompt", true)
	assert.Equal(t, lockBefore, workspace.baselines.manifest)
}

// A sync settings write that fails for any other reason (here a 500) stops
// the run with nothing changed, and the detach can be re-run.
func TestRunKeepsEverythingWhenTheSyncSettingsWriteFails(t *testing.T) {
	workspace, settings, options := singleDetach(t)
	lockBefore := workspace.baselines.manifest.Clone()
	settings.returnErrs["project"] = errors.New(`{"code":"internal","statusCode":500}`)
	archiver := &recordingArchiver{}
	options.Archiver, options.Archive, options.Yes = archiver, true, true

	err := Run(options)

	require.ErrorContains(t, err, "500")
	require.Len(t, settings.calls, 1)
	assert.Empty(t, archiver.archived)
	workspace.requireTracked(t, "prompt", true)
	assert.Equal(t, lockBefore, workspace.baselines.manifest)
}

// Without --yes and without a terminal, detach cannot ask whether to go on
// without the ownership rule, so it stops and deletes nothing.
func TestRunStopsWithoutYesWhenSyncSettingsAreUnavailable(t *testing.T) {
	workspace, settings, options := singleDetach(t)
	lockBefore := workspace.baselines.manifest.Clone()
	settings.returnErrs["project"] = notFound()

	err := Run(options)

	require.ErrorContains(t, err, "--yes")
	workspace.requireTracked(t, "prompt", true)
	assert.Equal(t, lockBefore, workspace.baselines.manifest)
}

// On a terminal, a 404 from the sync settings warns that LaunchDarkly will
// not record the owner, and asks a second question before anything is
// deleted, on the stream of the detach confirmation. Only that stream is a
// terminal here, so a question or a cancel line on the error output fails.
func TestRunAsksAgainWhenSyncSettingsAreUnavailable(t *testing.T) {
	tests := map[string]struct {
		secondAnswer string
		wantDetached bool
	}{
		"no keeps everything": {secondAnswer: "n\n", wantDetached: false},
		"yes detaches":        {secondAnswer: "y\n", wantDetached: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			workspace, settings, options := singleDetach(t)
			lockBefore := workspace.baselines.manifest.Clone()
			settings.returnErrs["project"] = notFound()
			var output, errorOutput bytes.Buffer
			options.Output, options.ErrorOutput = &output, &errorOutput
			interactive(&options, "y\n"+test.secondAnswer)

			require.NoError(t, Run(options))

			require.Len(t, settings.calls, 1)
			assert.Contains(t, errorOutput.String(), "ownership")
			assert.Equal(t, 2, strings.Count(output.String(), "[y/N]"),
				"both questions are on the stream of the detach confirmation")
			assert.Contains(t, output.String(), "Detach anyway? [y/N]")
			assert.NotContains(t, errorOutput.String(), "[y/N]")
			workspace.requireTracked(t, "prompt", !test.wantDetached)
			if !test.wantDetached {
				assert.Contains(t, output.String(), "Detach canceled.")
				assert.NotContains(t, errorOutput.String(), "Detach canceled.")
				assert.Equal(t, lockBefore, workspace.baselines.manifest)
			}
		})
	}
}

// Beta has no sync settings, and the read finds that out before any rule is
// written. When the user then cancels, alpha was not returned either.
func TestRunCanceledAfterARead404ReturnsNoProject(t *testing.T) {
	detach, options := newMultiProjectDetach(t)
	detach.settings.resolveErrs["beta"] = notFound()
	// The two answers cancel whichever question the settings warning comes with.
	interactive(&options, "y\nn\n")

	require.NoError(t, Run(options))

	assert.Empty(t, detach.settings.calls, "a canceled detach must not return any project to LaunchDarkly")
	assert.Contains(t, detach.output.String()+detach.errOutput.String(), "Detach canceled.")
	_, named := lineWith(detach.errOutput.String(), "beta")
	assert.True(t, named, "the settings-unavailable warning names beta: %q", detach.errOutput.String())
	detach.requireUnchanged(t)
}

// With --yes, a read 404 in beta warns and the detach goes on. Only alpha,
// whose settings exist, gets a rule.
func TestRunWithYesAfterARead404ReturnsOnlyTheOtherProject(t *testing.T) {
	detach, options := newMultiProjectDetach(t)
	detach.settings.resolveErrs["beta"] = notFound()
	options.Yes = true

	require.NoError(t, Run(options))

	assert.Equal(t, []string{"alpha"}, detach.settings.returnedProjects())
	_, named := lineWith(detach.errOutput.String(), "beta")
	assert.True(t, named, "the settings-unavailable warning names beta: %q", detach.errOutput.String())
	detach.requireDetached(t)
}

// Every read succeeds, so detach writes. The write of beta then returns a
// 404 (the settings went away between the read and the write), after alpha
// was returned to LaunchDarkly. When the user cancels, the output says that
// alpha's variations were already returned.
func TestRunCanceledAfterAWrite404SaysTheOtherProjectWasReturned(t *testing.T) {
	detach, options := newMultiProjectDetach(t)
	detach.settings.returnErrs["beta"] = notFound()
	interactive(&options, "y\nn\n")

	require.NoError(t, Run(options))

	require.Len(t, detach.settings.calls, 2)
	output := detach.output.String() + detach.errOutput.String()
	assert.Contains(t, output, "Detach canceled.")
	_, says := lineWith(output, "alpha", "already")
	assert.True(t, says, "the output says alpha was already returned to LaunchDarkly: %q", output)
	detach.requireUnchanged(t)
}

// With --yes, a write 404 in beta warns, names beta, and detaches both.
func TestRunWithYesAfterAWrite404StillDetaches(t *testing.T) {
	detach, options := newMultiProjectDetach(t)
	detach.settings.returnErrs["beta"] = notFound()
	options.Yes = true

	require.NoError(t, Run(options))

	assert.Equal(t, []string{"alpha", "beta"}, detach.settings.returnedProjects())
	_, named := lineWith(detach.errOutput.String(), "beta", "ownership")
	assert.True(t, named, "the settings-unavailable warning names beta: %q", detach.errOutput.String())
	detach.requireDetached(t)
}

// A 403 in beta comes after alpha was returned to LaunchDarkly. The problem
// or the output says so.
func TestRunForbiddenInTheSecondProjectSaysTheFirstWasReturned(t *testing.T) {
	detach, options := newMultiProjectDetach(t)
	detach.settings.returnErrs["beta"] = forbidden()
	options.Yes = true

	err := Run(options)

	require.ErrorIs(t, err, syncapi.ErrSyncSettingsForbidden)
	require.Len(t, detach.settings.calls, 2)
	text := err.Error() + "\n" + detach.output.String() + "\n" + detach.errOutput.String()
	_, says := lineWith(text, "alpha", "already")
	assert.True(t, says, "the problem or the output says alpha was already returned: %q", text)
	detach.requireUnchanged(t)
}

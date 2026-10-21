package prompt_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lderrors "github.com/launchdarkly/ldcli/internal/errors"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	"github.com/launchdarkly/ldcli/internal/sync/api/apitest"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

func decodeReports(t *testing.T, bodies []string) []syncapi.SyncReport {
	t.Helper()
	reports := make([]syncapi.SyncReport, 0, len(bodies))
	for _, body := range bodies {
		var report syncapi.SyncReport
		require.NoError(t, json.Unmarshal([]byte(body), &report), body)
		reports = append(reports, report)
	}
	return reports
}

func TestPromptAddMakesVariationCodeOnly(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	stdout, _, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "code-only", "--no-input")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support/default","direction":"code_canonical"}],
		"deletions": []
	}`, api.settingsPatches[0])
	assert.Contains(t, stdout, "The local files are now the source of truth for the prompts of these variations.")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPromptAddWithUIAndCodeMakesVariationBoth(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	stdout, _, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "ui-and-code", "--no-input")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support/default","direction":"both"}],
		"deletions": []
	}`, api.settingsPatches[0])
	assert.Contains(t, stdout, "The LaunchDarkly UI and the local files can now both change these variations.")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPromptAddNeedsSourceOfTruth(t *testing.T) {
	tests := map[string][]string{
		"with no input":    {"--no-input"},
		"without terminal": {},
	}
	for name, flags := range tests {
		t.Run(name, func(t *testing.T) {
			root := initRepository(t)
			api := &directAPI{variation: pointer(variation("Server"))}

			_, _, err := runPrompt(t, root, api, append([]string{"add", "production/support/default"}, flags...)...)

			require.ErrorContains(t, err, "add needs a source of truth. Use --source-of-truth ui-and-code or code-only.")
			assert.Empty(t, api.settingsPatches)
			exists, err := synclocal.NewStore(root).Exists()
			require.NoError(t, err)
			assert.False(t, exists)
		})
	}
}

func TestPromptAddRejectsUnknownSourceOfTruth(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	_, _, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "launchdarkly", "--no-input")

	require.ErrorContains(t, err, `the source of truth must be ui-and-code or code-only, not "launchdarkly"`)
	assert.Empty(t, api.requests)
}

func TestPromptAddRemovedNoAdopt(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	_, _, err := runPrompt(t, root, api, "add", "production/support/default", "--no-adopt", "--no-input")

	require.ErrorContains(t, err, "unknown flag: --no-adopt")
}

func TestPromptSourceOfTruthChangesRuleAndKeepsFile(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	stdout, _, err := runPrompt(t, root, api, "source-of-truth", "code-only", "production/support/default")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support/default","direction":"code_canonical"}],
		"deletions": []
	}`, api.settingsPatches[0])
	assert.Contains(t, stdout, "The local files are now the source of truth for the prompts of these variations.")
	assert.Empty(t, api.reports, "source-of-truth does not sync")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPromptSourceOfTruthNeedsSyncedVariation(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "source-of-truth", "ui-and-code", "production/support/other")

	require.ErrorContains(t, err, "variation production/support/other does not sync in this workspace.")
	assert.Empty(t, api.settingsPatches)
}

func TestPromptSourceOfTruthRejectsDuplicateAndUnknownValue(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "source-of-truth", "both", "production/support/default")
	require.ErrorContains(t, err, `the source of truth must be ui-and-code or code-only, not "both"`)

	_, _, err = runPrompt(t, root, api, "source-of-truth", "code-only", "production/support/default", "production/support/default")
	require.ErrorContains(t, err, "variation production/support/default was selected more than once")
	assert.Empty(t, api.settingsPatches)
}

// failingProjectSettingsAPI fails each sync settings write of one project.
type failingProjectSettingsAPI struct {
	*directAPI
	projectKey string
}

func (api failingProjectSettingsAPI) serve(request apitest.Request) ([]byte, error) {
	if request.Method == http.MethodPatch && strings.HasPrefix(request.Path, "/api/v2/projects/"+api.projectKey+"/configs/sync/settings") {
		return nil, lderrors.NewError(`{"code":"internal","message":"boom","statusCode":500}`)
	}
	return api.directAPI.serve(request)
}

func TestPromptSourceOfTruthNamesChangedProjectsWhenLaterProjectFails(t *testing.T) {
	root := initRepository(t)
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{
		{ProjectKey: "production", ConfigKey: "support", Variation: variation("Production")},
		{ProjectKey: "staging", ConfigKey: "support", Variation: variation("Staging")},
	})
	require.NoError(t, err)
	api := failingProjectSettingsAPI{directAPI: &directAPI{}, projectKey: "staging"}

	_, stderr, err := runPrompt(t, root, api, "source-of-truth", "code-only", "production/support/default", "staging/support/default")

	require.ErrorContains(t, err, "boom")
	assert.Contains(t, stderr, "The source of truth changed for project production only.")
	assert.Len(t, api.settingsPatches, 1)
}

func TestPromptSourceOfTruthFailsWhenSyncSettingsAreUnavailable(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation:        pointer(baseline),
		settingsPatchErr: lderrors.NewError(`{"code":"not_found","message":"Not found","statusCode":404}`),
	}

	_, _, err := runPrompt(t, root, api, "source-of-truth", "code-only", "production/support/default")

	require.ErrorContains(t, err, `update sync settings for project "production": sync settings are not available`)
}

func TestPromptSourceOfTruthOverridesWildcardWithUIAndCode(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	stdout, _, err := runPrompt(t, root, api, "source-of-truth", "ui-and-code", "production/support/default")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [{"resourceKind":"variation","resourcePattern":"support/default","direction":"both"}],
		"deletions": []
	}`, api.settingsPatches[0])
	assert.Contains(t, stdout, "The LaunchDarkly UI and the local files can now both change these variations.")
}

func TestPromptAddWarnsWhenSyncSettingsAreUnavailable(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{
		variation:        pointer(variation("Server")),
		settingsPatchErr: lderrors.NewError(`{"code":"not_found","message":"Not found","statusCode":404}`),
	}

	stdout, stderr, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "code-only", "--no-input")

	require.NoError(t, err)
	assert.Len(t, api.settingsPatches, 1)
	assert.Contains(t, stderr, "Warning: sync settings are not available for project production.")
	assert.NotContains(t, stdout, "source of truth")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPromptAddFailsWhenSyncSettingsAreForbidden(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{
		variation:        pointer(variation("Server")),
		settingsPatchErr: lderrors.NewError(`{"code":"forbidden","message":"Access denied","statusCode":403}`),
	}

	_, _, err := runPrompt(t, root, api, "add", "production/support/default", "--source-of-truth", "code-only", "--no-input")

	require.ErrorContains(t, err, "Access denied")
	exists, err := synclocal.NewStore(root).Exists()
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestPromptAddDryRunWritesNoRule(t *testing.T) {
	root := initRepository(t)
	api := &directAPI{variation: pointer(variation("Server"))}

	_, _, err := runPrompt(t, root, api, "add", "production/support/default", "--no-input", "--dry-run")

	require.NoError(t, err)
	assert.Empty(t, api.settingsPatches)
	assert.Empty(t, api.reports)
}

func TestPromptDetachDeletesExactRule(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "detach", "production/support/default", "--no-input")

	require.NoError(t, err)
	require.Len(t, api.settingsPatches, 1)
	assert.JSONEq(t, `{
		"upserts": [],
		"deletions": [{"resourceKind":"variation","resourcePattern":"support/default"}]
	}`, api.settingsPatches[0])
	assert.Empty(t, api.reports, "detach does not sync")
}

func TestPromptDetachWarnsWhenSyncSettingsAreUnavailable(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation:        pointer(baseline),
		settingsPatchErr: lderrors.NewError(`{"code":"not_found","message":"Not found","statusCode":404}`),
	}

	_, stderr, err := runPrompt(t, root, api, "detach", "production/support/default", "--no-input")

	require.NoError(t, err)
	assert.Contains(t, stderr, "Warning: sync settings are not available for project production.")
	exists, err := synclocal.NewStore(root).VariationExists("production", "support", "default")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestPromptReportsWrittenVersion(t *testing.T) {
	root := initRepository(t)
	baseline, local := variation("Baseline"), variation("Local")
	writeVariation(t, root, local, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline), version: 6}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	reports := decodeReports(t, api.reports)
	require.Len(t, reports, 1)
	assert.Equal(t, syncapi.SyncReport{
		Source: "git:example/repo",
		Resources: []syncapi.SyncReportResource{{
			ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default",
			Status: syncapi.ReportStatusSucceeded, Version: 7,
		}},
	}, reports[0])
}

func TestPromptReportsInSyncResourceWithoutVersion(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline), version: 6}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	reports := decodeReports(t, api.reports)
	require.Len(t, reports, 1)
	assert.Equal(t, []syncapi.SyncReportResource{{
		ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default", Status: syncapi.ReportStatusSucceeded,
	}}, reports[0].Resources)
}

func TestPromptReportsPulledResourceWithoutVersion(t *testing.T) {
	root := initRepository(t)
	baseline, server := variation("Baseline"), variation("Server")
	writeVariation(t, root, baseline, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(server)}

	_, _, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	reports := decodeReports(t, api.reports)
	require.Len(t, reports, 1)
	assert.Equal(t, []syncapi.SyncReportResource{{
		ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/default", Status: syncapi.ReportStatusSucceeded,
	}}, reports[0].Resources)
}

func TestPromptReportsFailedWrites(t *testing.T) {
	root := initRepository(t)
	firstBaseline, secondBaseline := variationWithKey("first", "First baseline"), variationWithKey("second", "Second baseline")
	firstLocal, secondLocal := variationWithKey("first", "First local"), variationWithKey("second", "Second local")
	_, err := synclocal.NewStore(root).Add([]synclocal.VariationFile{
		{ProjectKey: "production", ConfigKey: "support", Upsert: true, Variation: firstLocal},
		{ProjectKey: "production", ConfigKey: "support", Upsert: true, Variation: secondLocal},
	})
	require.NoError(t, err)
	writeManifestResources(t, root, firstBaseline, secondBaseline)
	api := &multiDirectAPI{
		variations: map[string]syncdomain.Variation{"first": firstBaseline, "second": secondBaseline},
		failKey:    "second",
	}

	_, _, err = runPrompt(t, root, api, "--yes")

	require.ErrorContains(t, err, "second")
	reports := decodeReports(t, api.reports)
	require.Len(t, reports, 1)
	require.Len(t, reports[0].Resources, 2)
	assert.Equal(t, syncapi.SyncReportResource{
		ResourceKind: syncdomain.KindVariation, ResourceLookupKey: "support/first", Status: syncapi.ReportStatusSucceeded,
	}, reports[0].Resources[0])
	failed := reports[0].Resources[1]
	assert.Equal(t, "support/second", failed.ResourceLookupKey)
	assert.Equal(t, syncapi.ReportStatusFailed, failed.Status)
	assert.Contains(t, failed.Error, `update config variation "second"`)
	assert.Zero(t, failed.Version)
}

func TestPromptReportFailureKeepsExitCode(t *testing.T) {
	root := initRepository(t)
	baseline, local := variation("Baseline"), variation("Local")
	writeVariation(t, root, local, false)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation: pointer(baseline),
		reportErr: lderrors.NewError(`{"code":"not_found","message":"Not found","statusCode":404}`),
	}

	_, stderr, err := runPrompt(t, root, api, "--yes")

	require.NoError(t, err)
	assert.Len(t, api.reports, 1)
	assert.Contains(t, stderr, "Warning: sync did not send its report to LaunchDarkly for project production")
	assert.Equal(t, local.Name, api.variation.Name)
	assertManifestFingerprint(t, root, local)
}

func TestPromptDryRunSendsNoReport(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "--dry-run")

	require.NoError(t, err)
	assert.Empty(t, api.reports)
}

func TestPromptCanceledSyncSendsNoReport(t *testing.T) {
	root := initRepository(t)
	baseline := variation("Baseline")
	writeVariation(t, root, variation("Local"), false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(variation("Server"))}

	// The local and the server changes conflict, and --conflict=abort stops
	// the sync before any write.
	_, _, _ = runPrompt(t, root, api, "--yes", "--no-input", "--conflict=abort")

	assert.Empty(t, api.reports)
}

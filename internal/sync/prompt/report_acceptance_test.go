package prompt_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
)

type sentReport struct {
	Source    string `json:"source"`
	Resources []struct {
		ResourceLookupKey string `json:"resourceLookupKey"`
		Status            string `json:"status"`
		Error             string `json:"error"`
	} `json:"resources"`
}

func decodeReports(t *testing.T, bodies []string) []sentReport {
	t.Helper()
	reports := make([]sentReport, 0, len(bodies))
	for _, body := range bodies {
		var report sentReport
		require.NoError(t, json.Unmarshal([]byte(body), &report), body)
		reports = append(reports, report)
	}
	return reports
}

func TestPromptReportsEachSync(t *testing.T) {
	root := initRepository(t)
	baseline, local := variation("Baseline"), variation("Local")
	writeVariation(t, root, local, false)
	writeManifest(t, root, baseline)
	api := &directAPI{variation: pointer(baseline)}

	_, _, err := runPrompt(t, root, api, "--yes")
	require.NoError(t, err)
	_, _, err = runPrompt(t, root, api, "--yes")
	require.NoError(t, err)

	reports := decodeReports(t, api.reports)
	require.Len(t, reports, 2, "a sync without changes also sends a report")
	for _, report := range reports {
		assert.Equal(t, "git:example/repo", report.Source)
		require.Len(t, report.Resources, 1)
		assert.Equal(t, "support/default", report.Resources[0].ResourceLookupKey)
		assert.Equal(t, "succeeded", report.Resources[0].Status)
	}
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
	assert.Equal(t, "support/first", reports[0].Resources[0].ResourceLookupKey)
	assert.Equal(t, "succeeded", reports[0].Resources[0].Status)
	failed := reports[0].Resources[1]
	assert.Equal(t, "support/second", failed.ResourceLookupKey)
	assert.Equal(t, "failed", failed.Status)
	assert.Contains(t, failed.Error, `update config variation "second"`)
}

func TestPromptReportFailureKeepsExitCode(t *testing.T) {
	root := initRepository(t)
	baseline, local := variation("Baseline"), variation("Local")
	writeVariation(t, root, local, false)
	writeManifest(t, root, baseline)
	api := &directAPI{
		variation: pointer(baseline),
		reportErr: notFoundError,
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

	_, _, _ = runPrompt(t, root, api, "--yes", "--no-input", "--conflict=abort")

	assert.Empty(t, api.reports)
}

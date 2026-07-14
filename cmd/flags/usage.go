package flags

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/flagusage/enrich"
	"github.com/launchdarkly/ldcli/internal/flagusage/render"
	"github.com/launchdarkly/ldcli/internal/flagusage/scanner"
)

const (
	usageDirFlag            = "dir"
	usageFormatFlag         = "format"
	usageWrapperModulesFlag = "wrapper-modules"
	usageDefinitionsFlag    = "definitions"
	usageEvalWindowFlag     = "eval-window"
	usageEvalSeriesFlag     = "eval-series"
	usageExposuresFlag      = "exposures"
	usageContextKindsFlag   = "context-kinds"
	usageWidthFlag          = "width"
)

// NewUsageCmd reports where a project's feature flags are evaluated in code,
// enriched with each flag's per-environment status pulled from the LD API.
// It is a port of the standalone `flagpls` tool (github.com/launchdarkly-labs/flagpls).
func NewUsageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Long:  "Scan a source directory for feature flag evaluation call sites and report each flag's per-environment status, targeting, and (optionally) evaluation/exposure counts from the LaunchDarkly API.",
		RunE:  runUsage,
		Short: "Report feature flag usage in code, enriched with live flag status",
		Use:   "usage",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initUsageFlags(cmd)

	return cmd
}

func initUsageFlags(cmd *cobra.Command) {
	cmd.Flags().String(usageDirFlag, ".", "Directory to scan")
	cmd.Flags().String(usageFormatFlag, "text", "Output format: text, json")
	cmd.Flags().String(usageWrapperModulesFlag, "", "OVERRIDE (rarely needed): comma-separated wrapper module paths to force-track; modules are auto-discovered via node_modules")
	cmd.Flags().String(usageDefinitionsFlag, "", "OVERRIDE (rarely needed): dir of wrapper definition files; definitions are auto-discovered via node_modules — pass this only if deps aren't installed")
	cmd.Flags().StringSlice(cliflags.EnvsFlag, nil, cliflags.EnvsFlagDescription)
	_ = viper.BindPFlag(cliflags.EnvsFlag, cmd.Flags().Lookup(cliflags.EnvsFlag))
	cmd.Flags().String(usageEvalWindowFlag, "", "Evaluation lookback window (e.g. 24h, 6h); default 168h (7d)")
	cmd.Flags().Bool(usageEvalSeriesFlag, false, "Fetch the per-bucket evaluation timeseries (daily, or hourly for windows <24h); default fetches window totals only via a single batched call per flag")
	cmd.Flags().Bool(usageExposuresFlag, false, "Fetch true unique-context counts per context kind (the figure a guarded release gates on), not just total evaluations")
	cmd.Flags().String(usageContextKindsFlag, "", "With --exposures, restrict to these comma-separated context kinds (default: auto-discover the flag's kinds)")
	cmd.Flags().String(cliflags.FlagFlag, "", "Report only this flag — matches its key or a wrapper accessor name (for editor single-flag lookup)")
	cmd.Flags().Int(usageWidthFlag, 0, "Table width in columns; 0 = auto-detect the terminal")

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))
}

func runUsage(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString(usageDirFlag)
	format, _ := cmd.Flags().GetString(usageFormatFlag)
	wrapperModules, _ := cmd.Flags().GetString(usageWrapperModulesFlag)
	definitionsDir, _ := cmd.Flags().GetString(usageDefinitionsFlag)
	evalWindowRaw, _ := cmd.Flags().GetString(usageEvalWindowFlag)
	evalSeries, _ := cmd.Flags().GetBool(usageEvalSeriesFlag)
	exposures, _ := cmd.Flags().GetBool(usageExposuresFlag)
	contextKindsRaw, _ := cmd.Flags().GetString(usageContextKindsFlag)
	flagFilter, _ := cmd.Flags().GetString(cliflags.FlagFlag)
	width, _ := cmd.Flags().GetInt(usageWidthFlag)

	project := viper.GetString(cliflags.ProjectFlag)
	token := viper.GetString(cliflags.AccessTokenFlag)
	baseURL := viper.GetString(cliflags.BaseURIFlag)

	// --envs, then LD_ENVS, then the ldcli config file (all via viper); if none of
	// those set anything, fall back to live-discovering the project's critical
	// environments — the same signal `flagpls setup` persists, just resolved lazily
	// instead of requiring an explicit setup step.
	envs := viper.GetStringSlice(cliflags.EnvsFlag)
	if len(envs) == 0 {
		discovered, err := fetchCriticalEnvs(token, baseURL, project)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: couldn't auto-discover critical environments (%v); pass --envs explicitly\n", err)
		}
		envs = discovered
	}

	var evalWindow time.Duration
	if evalWindowRaw != "" {
		d, err := time.ParseDuration(evalWindowRaw)
		if err != nil {
			return fmt.Errorf("invalid --%s: %w", usageEvalWindowFlag, err)
		}
		evalWindow = d
	}

	scanResult, err := scanner.Scan(dir, scanner.ScanOptions{
		WrapperModules: splitNonEmpty(wrapperModules),
		DefinitionsDir: definitionsDir,
	})
	if err != nil {
		return fmt.Errorf("scanning %s: %w", dir, err)
	}

	if flagFilter != "" {
		filtered := *scanResult
		refs := make([]scanner.FlagReference, 0, len(scanResult.References))
		for _, ref := range scanResult.References {
			if ref.FlagKey == flagFilter || ref.WrapperName == flagFilter {
				refs = append(refs, ref)
			}
		}
		filtered.References = refs
		scanResult = &filtered
	}

	client := enrich.NewClient(token, baseURL)
	details, err := enrich.Enrich(client, scanResult, enrich.Options{
		ProjectKey:   project,
		Environments: envs,
		EvalWindow:   evalWindow,
		Series:       evalSeries,
		Exposures:    exposures,
		ContextKinds: splitNonEmpty(contextKindsRaw),
	})
	if err != nil {
		return fmt.Errorf("enriching flag usage: %w", err)
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(details)
	}

	windowLabel := evalWindowRaw
	if windowLabel == "" {
		windowLabel = "7d"
	}
	render.Enriched(os.Stdout, details, windowLabel, envs, width)

	return nil
}

// fetchCriticalEnvs lists the project's environments and returns the keys marked
// `critical` in LaunchDarkly (the env-level "Critical environment" toggle). It hits
// the REST API directly rather than the generated api-client-go SDK because ldcli's
// current SDK version (v14) doesn't expose the Critical field on its Environment
// model (added in a later API/SDK version).
func fetchCriticalEnvs(token, baseURL, projectKey string) ([]string, error) {
	u, err := url.JoinPath(baseURL, "api/v2/projects", projectKey, "environments")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, u+"?limit=200", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listing environments failed (HTTP %d)", resp.StatusCode)
	}

	var body struct {
		Items []struct {
			Key      string `json:"key"`
			Critical bool   `json:"critical"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	var critical []string
	for _, e := range body.Items {
		if e.Critical {
			critical = append(critical, e.Key)
		}
	}
	return critical, nil
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

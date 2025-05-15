package sourcemaps

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const (
	apiKeyFlag     = "api-key"
	appVersionFlag = "app-version"
	pathFlag       = "path"
	basePathFlag   = "base-path"
	backendUrlFlag = "backend-url"

	defaultPath       = "."
	defaultBackendUrl = "https://app.launchdarkly.com"

	npmPackage = "@highlight-run/sourcemap-uploader"
)

func NewUploadCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload sourcemaps",
		Long:  "Upload JavaScript sourcemaps to LaunchDarkly for error monitoring",
		RunE:  runE(client),
	}

	cmd.Flags().String(apiKeyFlag, "", "The LaunchDarkly API key")
	_ = cmd.MarkFlagRequired(apiKeyFlag)
	_ = cmd.Flags().SetAnnotation(apiKeyFlag, "required", []string{"true"})
	_ = viper.BindPFlag(apiKeyFlag, cmd.Flags().Lookup(apiKeyFlag))

	cmd.Flags().String(appVersionFlag, "", "The current version of your deploy")
	_ = viper.BindPFlag(appVersionFlag, cmd.Flags().Lookup(appVersionFlag))

	cmd.Flags().String(pathFlag, defaultPath, "Sets the directory of where the sourcemaps are")
	_ = viper.BindPFlag(pathFlag, cmd.Flags().Lookup(pathFlag))

	cmd.Flags().String(basePathFlag, "", "An optional base path for the uploaded sourcemaps")
	_ = viper.BindPFlag(basePathFlag, cmd.Flags().Lookup(basePathFlag))

	cmd.Flags().String(backendUrlFlag, defaultBackendUrl, "An optional backend url for self-hosted deployments")
	_ = viper.BindPFlag(backendUrlFlag, cmd.Flags().Lookup(backendUrlFlag))

	return cmd
}

func runE(client resources.Client) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		var tracker analytics.Tracker = &analytics.NoopClient{}
		if analyticsTrackerFn, ok := cmd.Root().Annotations["analytics_tracker_fn"]; ok {
			trackerFn := analytics.NoopClientFn{}.Tracker()
			if analyticsTrackerFn == "client" {
				trackerFn = analytics.ClientFn{
					ID:      "ldcli",
					Version: "dev",
				}.Tracker
			}
			tracker = trackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
		}

		tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
			cmd,
			"sourcemaps",
			map[string]interface{}{
				"action": "upload",
			}))

		apiKey := viper.GetString(apiKeyFlag)
		appVersion := viper.GetString(appVersionFlag)
		path := viper.GetString(pathFlag)
		basePath := viper.GetString(basePathFlag)
		backendUrl := viper.GetString(backendUrlFlag)

		if apiKey == "" {
			return fmt.Errorf("api key cannot be empty")
		}

		if err := checkNodeInstalled(); err != nil {
			return fmt.Errorf("Node.js is required to upload sourcemaps: %v", err)
		}

		npxArgs := []string{npmPackage, "upload", "--apiKey", apiKey}

		if appVersion != "" {
			npxArgs = append(npxArgs, "--appVersion", appVersion)
		}

		if path != defaultPath {
			npxArgs = append(npxArgs, "--path", path)
		}

		if basePath != "" {
			npxArgs = append(npxArgs, "--basePath", basePath)
		}

		if backendUrl != defaultBackendUrl {
			npxArgs = append(npxArgs, "--backendUrl", backendUrl)
		}

		fmt.Printf("Starting to upload source maps from %s using %s\n", path, npmPackage)

		execCmd := exec.Command("npx", npxArgs...)
		var stdout, stderr bytes.Buffer
		execCmd.Stdout = &stdout
		execCmd.Stderr = &stderr
		execCmd.Env = os.Environ()

		err := execCmd.Run()
		fmt.Print(stdout.String())

		if err != nil {
			fmt.Print(stderr.String())
			return fmt.Errorf("failed to upload sourcemaps: %v", err)
		}

		fmt.Println("Successfully uploaded all sourcemaps")
		return nil
	}
}

func checkNodeInstalled() error {
	_, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("Node.js is not installed or not in PATH: %v", err)
	}

	_, err = exec.LookPath("npx")
	if err != nil {
		return fmt.Errorf("npx is not installed or not in PATH: %v", err)
	}

	return nil
}

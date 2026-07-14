package symbols

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const (
	typeFlag       = "type"
	appVersionFlag = "app-version"
	pathFlag       = "path"
	basePathFlag   = "base-path"
	backendUrlFlag = "backend-url"

	defaultPath       = "."
	defaultBackendUrl = "https://pri.observability.app.launchdarkly.com"

	// typeReactNative is the only symbol type supported today. React Native
	// Hermes/Metro sourcemaps are ordinary JavaScript sourcemaps; they are
	// uploaded through the dedicated symbol path and land under the same key the
	// symbolication backend already reads.
	typeReactNative = "react-native"

	// getSymbolUrlsQuery uses the dedicated `get_symbol_upload_urls_ld` query
	// (separate from `sourcemaps upload`) so symbol uploads travel over the
	// symbol endpoint, which accepts larger, multi-segment uploads.
	getSymbolUrlsQuery = `
	  query GetSymbolUploadUrls($api_key: String!, $project_id: String!, $paths: [String!]!) {
	    get_symbol_upload_urls_ld(
			api_key: $api_key
			project_id: $project_id
			paths: $paths
		)
	  }
	`
)

// reactNativeUploadSuffixes are the files produced by `react-native bundle`:
// `main.jsbundle`(.map) on iOS and `index.android.bundle`(.map) on Android.
// The minified bundle is uploaded alongside its map so `sourceMappingURL` and
// column offsets resolve during symbolication.
var reactNativeUploadSuffixes = []string{
	".jsbundle.map", ".jsbundle",
	".bundle.map", ".bundle",
}

type SymbolUrlsResponse struct {
	Data struct {
		GetSymbolUploadUrls []string `json:"get_symbol_upload_urls_ld"`
	} `json:"data"`
}

type SymbolFile struct {
	Path string
	Name string
}

func NewUploadCmd(client resources.Client, analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Use:   "upload",
		Short: "Upload symbol files",
		Long:  "Upload symbol files (for example, React Native sourcemaps) to LaunchDarkly for error monitoring",
		RunE:  runE(client),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(
				cmd,
				"symbols",
				map[string]interface{}{
					"action": cmd.Name(),
				}))
		},
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initFlags(cmd)

	return cmd
}

func runE(client resources.Client) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		symbolType := viper.GetString(typeFlag)
		if symbolType != typeReactNative {
			return fmt.Errorf("unsupported --type %q; supported types: %s", symbolType, typeReactNative)
		}

		projectKey := viper.GetString(cliflags.ProjectFlag)
		u, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/projects",
			projectKey,
		)
		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"GET",
			u,
			"application/json",
			nil,
			nil,
			false,
		)
		if err != nil {
			return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
		}

		var projectResult struct {
			ID string `json:"_id"`
		}
		if err = json.Unmarshal(res, &projectResult); err != nil {
			return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
		}
		if projectResult.ID == "" {
			return fmt.Errorf("project %s not found", projectKey)
		}

		appVersion := viper.GetString(appVersionFlag)
		path := viper.GetString(pathFlag)
		basePath := viper.GetString(basePathFlag)
		backendUrl := viper.GetString(backendUrlFlag)

		if backendUrl == "" {
			backendUrl = defaultBackendUrl
		}

		fmt.Printf("Starting to upload %s symbols from %s\n", symbolType, path)

		files, err := getAllSymbolFiles(path, symbolType)
		if err != nil {
			return fmt.Errorf("failed to find symbol files: %w", err)
		}

		if len(files) == 0 {
			return fmt.Errorf("no symbol files found in %s, is this the correct path?", path)
		}

		s3Keys := make([]string, 0, len(files))
		for _, file := range files {
			s3Keys = append(s3Keys, getS3Key(appVersion, basePath, file.Name))
		}

		uploadUrls, err := getSymbolUploadUrls(viper.GetString(cliflags.AccessTokenFlag), projectResult.ID, s3Keys, backendUrl)
		if err != nil {
			return fmt.Errorf("failed to get upload URLs: %w", err)
		}

		for i, file := range files {
			if err := uploadFile(file.Path, uploadUrls[i], file.Name); err != nil {
				return fmt.Errorf("failed to upload file %s: %w", file.Path, err)
			}
		}

		fmt.Println("Successfully uploaded all symbols")
		return nil
	}
}

func isReactNativeUploadFile(name string) bool {
	for _, suffix := range reactNativeUploadSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func getAllSymbolFiles(path, symbolType string) ([]SymbolFile, error) {
	// symbolType is validated by the caller; only react-native is supported.
	_ = symbolType

	var files []SymbolFile

	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !fileInfo.IsDir() {
		files = append(files, SymbolFile{
			Path: path,
			Name: filepath.Base(path),
		})
		return files, nil
	}

	err = filepath.WalkDir(path, func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && d.Name() == "node_modules" {
			return filepath.SkipDir
		}

		if !d.IsDir() && isReactNativeUploadFile(filePath) {
			relPath, err := filepath.Rel(path, filePath)
			if err != nil {
				return err
			}

			files = append(files, SymbolFile{
				Path: filePath,
				Name: relPath,
			})
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no React Native symbol files found (looked for *.jsbundle, *.jsbundle.map, *.bundle, *.bundle.map). Please double check that you have generated sourcemaps for your app")
	}

	return files, nil
}

func getS3Key(version, basePath, fileName string) string {
	if version == "" {
		version = "unversioned"
	}

	if basePath != "" && !strings.HasSuffix(basePath, "/") {
		basePath = basePath + "/"
	}

	return fmt.Sprintf("%s/%s%s", version, basePath, fileName)
}

func getSymbolUploadUrls(apiKey, projectID string, paths []string, backendUrl string) ([]string, error) {
	variables := map[string]interface{}{
		"api_key":    apiKey,
		"project_id": projectID,
		"paths":      paths,
	}

	reqBody, err := json.Marshal(map[string]interface{}{
		"query":     getSymbolUrlsQuery,
		"variables": variables,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", backendUrl, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var urlsResp SymbolUrlsResponse
	if err := json.Unmarshal(body, &urlsResp); err != nil {
		return nil, err
	}

	if len(urlsResp.Data.GetSymbolUploadUrls) == 0 {
		return nil, fmt.Errorf("unable to generate symbol upload urls %w", err)
	}

	return urlsResp.Data.GetSymbolUploadUrls, nil
}

func uploadFile(filePath, uploadUrl, name string) error {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", uploadUrl, bytes.NewBuffer(fileContent))
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload failed with status code: %d", resp.StatusCode)
	}

	fmt.Printf("[LaunchDarkly] Uploaded %s to %s\n", filePath, name)
	return nil
}

func initFlags(cmd *cobra.Command) {
	cmd.Flags().String(typeFlag, "", fmt.Sprintf("The symbol type to upload (supported: %s)", typeReactNative))
	_ = cmd.MarkFlagRequired(typeFlag)
	_ = cmd.Flags().SetAnnotation(typeFlag, "required", []string{"true"})
	_ = viper.BindPFlag(typeFlag, cmd.Flags().Lookup(typeFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(appVersionFlag, "", "The current version of your deploy")
	_ = viper.BindPFlag(appVersionFlag, cmd.Flags().Lookup(appVersionFlag))

	cmd.Flags().String(pathFlag, defaultPath, "Sets the directory of where the symbol files are")
	_ = viper.BindPFlag(pathFlag, cmd.Flags().Lookup(pathFlag))

	cmd.Flags().String(basePathFlag, "", "An optional base path for the uploaded symbol files")
	_ = viper.BindPFlag(basePathFlag, cmd.Flags().Lookup(basePathFlag))

	cmd.Flags().String(backendUrlFlag, defaultBackendUrl, "An optional backend url for self-hosted deployments")
	_ = viper.BindPFlag(backendUrlFlag, cmd.Flags().Lookup(backendUrlFlag))
}

package sourcemaps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

	verifyApiKeyQuery = `
	  query ApiKeyToOrgID($api_key: String!) {
	    api_key_to_org_id(api_key: $api_key)
	  }
	`

	getSourceMapUrlsQuery = `
	  query GetSourceMapUploadUrls($api_key: String!, $paths: [String!]!) {
	    get_source_map_upload_urls(api_key: $api_key, paths: $paths)
	  }
	`
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

		organizationId, err := verifyApiKey(apiKey, backendUrl)
		if err != nil {
			return err
		}

		fmt.Printf("Starting to upload source maps from %s\n", path)

		fileList, err := getAllSourceMapFiles(path)
		if err != nil {
			return err
		}

		if len(fileList) == 0 {
			return fmt.Errorf("no source maps found in %s, is this the correct path?", path)
		}

		s3Keys := make([]string, len(fileList))
		for i, file := range fileList {
			s3Keys[i] = getS3Key(organizationId, appVersion, basePath, file.Name)
		}

		uploadUrls, err := getSourceMapUploadUrls(apiKey, s3Keys, backendUrl)
		if err != nil {
			return err
		}

		for i, file := range fileList {
			err = uploadFile(file.Path, uploadUrls[i], file.Name)
			if err != nil {
				return err
			}
		}

		fmt.Println("Successfully uploaded all sourcemaps")
		return nil
	}
}

func verifyApiKey(apiKey, backendUrl string) (string, error) {
	variables := map[string]interface{}{
		"api_key": apiKey,
	}

	body, err := json.Marshal(map[string]interface{}{
		"query":     verifyApiKeyQuery,
		"variables": variables,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", backendUrl, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result struct {
		Data struct {
			ApiKeyToOrgID string `json:"api_key_to_org_id"`
		} `json:"data"`
	}

	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return "", err
	}

	if result.Data.ApiKeyToOrgID == "" || result.Data.ApiKeyToOrgID == "0" {
		return "", fmt.Errorf("invalid api key")
	}

	return result.Data.ApiKeyToOrgID, nil
}

func getSourceMapUploadUrls(apiKey string, paths []string, backendUrl string) ([]string, error) {
	variables := map[string]interface{}{
		"api_key": apiKey,
		"paths":   paths,
	}

	body, err := json.Marshal(map[string]interface{}{
		"query":     getSourceMapUrlsQuery,
		"variables": variables,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", backendUrl, bytes.NewBuffer(body))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			GetSourceMapUploadUrls []string `json:"get_source_map_upload_urls"`
		} `json:"data"`
	}

	err = json.Unmarshal(respBody, &result)
	if err != nil {
		return nil, err
	}

	if len(result.Data.GetSourceMapUploadUrls) == 0 {
		return nil, fmt.Errorf("unable to generate source map upload urls")
	}

	return result.Data.GetSourceMapUploadUrls, nil
}

type SourceMapFile struct {
	Path string
	Name string
}

func getAllSourceMapFiles(path string) ([]SourceMapFile, error) {
	var fileList []SourceMapFile

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	if fileInfo.IsDir() {
		err = filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				if info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}

			if strings.HasSuffix(info.Name(), ".js.map") {
				relPath, err := filepath.Rel(absPath, path)
				if err != nil {
					return err
				}

				fileList = append(fileList, SourceMapFile{
					Path: path,
					Name: relPath,
				})
			}

			return nil
		})

		if err != nil {
			return nil, err
		}
	} else {
		if strings.HasSuffix(fileInfo.Name(), ".js.map") {
			fileList = append(fileList, SourceMapFile{
				Path: absPath,
				Name: fileInfo.Name(),
			})
		}
	}

	return fileList, nil
}

func getS3Key(organizationId, version, basePath, fileName string) string {
	if version == "" {
		version = "unversioned"
	}

	if basePath != "" && !strings.HasSuffix(basePath, "/") {
		basePath = basePath + "/"
	}

	return fmt.Sprintf("%s/%s/%s%s", organizationId, version, basePath, fileName)
}

func uploadFile(filePath, uploadUrl, name string) error {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return err
	}

	_, err = part.Write(fileContent)
	if err != nil {
		return err
	}

	err = writer.Close()
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", uploadUrl, bytes.NewReader(fileContent))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to upload %s: %s", name, resp.Status)
	}

	fmt.Printf("Uploaded %s\n", name)
	return nil
}

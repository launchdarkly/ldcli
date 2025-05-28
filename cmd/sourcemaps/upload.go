package sourcemaps

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
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

const (
	appVersionFlag = "app-version"
	pathFlag       = "path"
	basePathFlag   = "base-path"
	backendUrlFlag = "backend-url"

	defaultPath       = "."
	defaultBackendUrl = "https://pri.observability.app.launchdarkly.com"

	getSourceMapUrlsQuery = `
	  query GetSourceMapUploadUrls($api_key: String!, $paths: [String!]!) {
	    get_source_map_upload_urls_ld(
			api_key: String!
			project_id: String!
			paths: [String!]!
		): [String!]!
	  }
	`
)

type ApiKeyResponse struct {
	Data struct {
		Credential struct {
			ProjectID string `json:"project_id"`
			APIKey    string `json:"api_key"`
		} `json:"ld_credential"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type SourceMapUrlsResponse struct {
	Data struct {
		GetSourceMapUploadUrls []string `json:"get_source_map_upload_urls"`
	} `json:"data"`
}

type SourceMapFile struct {
	Path string
	Name string
}

func NewUploadCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Use:   "upload",
		Short: "Upload sourcemaps",
		Long:  "Upload JavaScript sourcemaps to LaunchDarkly for error monitoring",
		RunE:  runE(client),
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initFlags(cmd)

	return cmd
}

func runE(client resources.Client) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
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
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
		}

		var projectResult struct {
			ID string `json:"_id"`
		}
		if err = json.Unmarshal(res, &projectResult); err != nil {
			return output.NewCmdOutputError(err, viper.GetString(cliflags.OutputFlag))
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

		fmt.Printf("Starting to upload source maps from %s\n", path)

		files, err := getAllSourceMapFiles(path)
		if err != nil {
			return fmt.Errorf("failed to find sourcemap files: %w", err)
		}

		if len(files) == 0 {
			return fmt.Errorf("no source maps found in %s, is this the correct path?", path)
		}

		s3Keys := make([]string, 0, len(files))
		for _, file := range files {
			s3Keys = append(s3Keys, getS3Key(projectResult.ID, appVersion, basePath, file.Name))
		}

		uploadUrls, err := getSourceMapUploadUrls(viper.GetString(cliflags.AccessTokenFlag), s3Keys, backendUrl)
		if err != nil {
			return fmt.Errorf("failed to get upload URLs: %w", err)
		}

		for i, file := range files {
			if err := uploadFile(file.Path, uploadUrls[i], file.Name); err != nil {
				return fmt.Errorf("failed to upload file %s: %w", file.Path, err)
			}
		}

		fmt.Println("Successfully uploaded all sourcemaps")
		return nil
	}
}

func getAllSourceMapFiles(path string) ([]SourceMapFile, error) {
	var files []SourceMapFile
	routeGroupPattern := regexp.MustCompile(`\(.+?\)/`)

	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !fileInfo.IsDir() {
		files = append(files, SourceMapFile{
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

		if !d.IsDir() && (strings.HasSuffix(filePath, ".js.map") || strings.HasSuffix(filePath, ".js")) {
			relPath, err := filepath.Rel(path, filePath)
			if err != nil {
				return err
			}

			files = append(files, SourceMapFile{
				Path: filePath,
				Name: relPath,
			})

			routeGroupRemovedPath := routeGroupPattern.ReplaceAllString(relPath, "")
			if routeGroupRemovedPath != relPath {
				files = append(files, SourceMapFile{
					Path: filePath,
					Name: routeGroupRemovedPath,
				})
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no .js.map files found. Please double check that you have generated sourcemaps for your app")
	}

	return files, nil
}

func getS3Key(organizationID, version, basePath, fileName string) string {
	if version == "" {
		version = "unversioned"
	}

	if basePath != "" && !strings.HasSuffix(basePath, "/") {
		basePath = basePath + "/"
	}

	return fmt.Sprintf("%s/%s/%s%s", organizationID, version, basePath, fileName)
}

func getSourceMapUploadUrls(apiKey string, paths []string, backendUrl string) ([]string, error) {
	variables := map[string]interface{}{
		"api_key": apiKey,
		"paths":   paths,
	}

	reqBody, err := json.Marshal(map[string]interface{}{
		"query":     getSourceMapUrlsQuery,
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

	var urlsResp SourceMapUrlsResponse
	if err := json.Unmarshal(body, &urlsResp); err != nil {
		return nil, err
	}

	if len(urlsResp.Data.GetSourceMapUploadUrls) == 0 {
		return nil, fmt.Errorf("unable to generate source map upload urls")
	}

	return urlsResp.Data.GetSourceMapUploadUrls, nil
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
	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(appVersionFlag, "", "The current version of your deploy")
	_ = viper.BindPFlag(appVersionFlag, cmd.Flags().Lookup(appVersionFlag))

	cmd.Flags().String(pathFlag, defaultPath, "Sets the directory of where the sourcemaps are")
	_ = viper.BindPFlag(pathFlag, cmd.Flags().Lookup(pathFlag))

	cmd.Flags().String(basePathFlag, "", "An optional base path for the uploaded sourcemaps")
	_ = viper.BindPFlag(basePathFlag, cmd.Flags().Lookup(basePathFlag))

	cmd.Flags().String(backendUrlFlag, defaultBackendUrl, "An optional backend url for self-hosted deployments")
	_ = viper.BindPFlag(backendUrlFlag, cmd.Flags().Lookup(backendUrlFlag))
}

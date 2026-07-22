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
	symbolsIdFlag  = "symbols-id"
	pathFlag       = "path"
	basePathFlag   = "base-path"
	backendUrlFlag = "backend-url"

	defaultPath       = "."
	defaultBackendUrl = "https://pri.observability.app.launchdarkly.com"

	// reactNativeSymbolsIDPrefix is the storage "version" segment for symbols-id
	// addressed JS maps (Symbols Id Lane). Keys become _sym/js/id/<symbolsID>/<file>,
	// matching what the symbolication backend derives from the reported symbols id.
	reactNativeSymbolsIDPrefix = "_sym/js/id"

	// androidSymbolsIDPrefix is the equivalent Symbols Id Lane segment for Android
	// R8 / ProGuard mappings. Keys become _sym/android/id/<symbolsID>/mapping.txt.
	androidSymbolsIDPrefix = "_sym/android/id"

	// symbolsIDSidecarSuffix names the file written next to an artifact to record
	// its symbols id (the Metro plugin for React Native, the Gradle task for
	// Android), so `ldcli` can upload with the exact id the app reports without a
	// manual --symbols-id.
	symbolsIDSidecarSuffix = ".symbolsid"

	// androidMappingFileName is the R8/ProGuard mapping file `ldcli` discovers
	// for --type android.
	androidMappingFileName = "mapping.txt"

	// typeReactNative uploads React Native Hermes/Metro sourcemaps (ordinary
	// JavaScript sourcemaps).
	typeReactNative = "react-native"

	// typeAndroid uploads an Android R8/ProGuard `mapping.txt` for Java/Kotlin
	// stack-trace retrace.
	typeAndroid = "android"

	// typeAppleDSYM compiles Apple dSYM debug info into per-architecture .ldsm
	// symbol maps (keyed by build UUID) for iOS/macOS crash symbolication.
	typeAppleDSYM = "apple-dsym"

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
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
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
		Long:  "Upload symbol files (React Native sourcemaps, Android R8/ProGuard mappings, or Apple dSYMs) to LaunchDarkly for error monitoring",
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
		if !isSupportedType(symbolType) {
			return fmt.Errorf("unsupported --type %q; supported types: %s, %s, %s", symbolType, typeReactNative, typeAndroid, typeAppleDSYM)
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
		symbolsID := viper.GetString(symbolsIdFlag)
		path := viper.GetString(pathFlag)
		basePath := viper.GetString(basePathFlag)
		backendUrl := viper.GetString(backendUrlFlag)

		if backendUrl == "" {
			backendUrl = defaultBackendUrl
		}

		// Apple dSYMs take a dedicated path: they are compiled to per-arch .ldsm
		// symbol maps keyed by build UUID, ignoring the version/symbols-id lanes.
		if symbolType == typeAppleDSYM {
			fmt.Printf("Starting to upload %s symbols from %s\n", symbolType, path)
			return uploadAppleDSYMs(viper.GetString(cliflags.AccessTokenFlag), projectResult.ID, path, backendUrl)
		}

		symbolsIDPrefix := symbolsIDPrefixForType(symbolType)

		fmt.Printf("Starting to upload %s symbols from %s\n", symbolType, path)
		if symbolsID != "" {
			fmt.Printf("Using symbols id %s for all files (Symbols Id Lane: %s/%s)\n", symbolsID, symbolsIDPrefix, symbolsID)
		}

		files, err := getAllSymbolFiles(path, symbolType)
		if err != nil {
			return fmt.Errorf("failed to find symbol files: %w", err)
		}

		if len(files) == 0 {
			return fmt.Errorf("no symbol files found in %s, is this the correct path?", path)
		}

		// Symbols Id Lane: resolve the id per file so a single upload of multiple
		// platforms (e.g. iOS + Android maps in one dir) keys each artifact by the
		// id its app reports. An explicit --symbols-id overrides all files;
		// otherwise each artifact's *.symbolsid sidecar (or its sibling's — see
		// symbolsIDForArtifact) is used, falling back to the Version Lane
		// (version+basePath) when there is none.
		s3Keys := make([]string, 0, len(files))
		for _, file := range files {
			fileSymbolsID := symbolsID
			if fileSymbolsID == "" {
				fileSymbolsID = symbolsIDForArtifact(file.Path)
				if fileSymbolsID != "" {
					fmt.Printf("Using symbols id %s for %s (Symbols Id Lane: %s/%s)\n", fileSymbolsID, file.Name, symbolsIDPrefix, fileSymbolsID)
				}
			}
			s3Keys = append(s3Keys, getS3Key(symbolsIDPrefix, fileSymbolsID, appVersion, basePath, file.Name))
		}

		uploadUrls, err := getSymbolUploadUrls(viper.GetString(cliflags.AccessTokenFlag), projectResult.ID, s3Keys, backendUrl)
		if err != nil {
			return fmt.Errorf("failed to get upload URLs: %w", err)
		}

		// The loop below pairs each file with uploadUrls[i], so a short list
		// (fewer URLs than files) would panic. Require one URL per requested key.
		if len(uploadUrls) != len(files) {
			return fmt.Errorf("expected %d upload URLs but received %d", len(files), len(uploadUrls))
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

func isSupportedType(symbolType string) bool {
	return symbolType == typeReactNative || symbolType == typeAndroid || symbolType == typeAppleDSYM
}

// symbolsIDPrefixForType picks the Symbols Id Lane storage segment for the symbol
// type so JS and Android maps never collide in the same symbols-id namespace.
func symbolsIDPrefixForType(symbolType string) string {
	if symbolType == typeAndroid {
		return androidSymbolsIDPrefix
	}
	return reactNativeSymbolsIDPrefix
}

func isReactNativeUploadFile(name string) bool {
	for _, suffix := range reactNativeUploadSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// isSymbolUploadFile reports whether a discovered file should be uploaded for
// the given symbol type: React Native bundles/maps, or an Android mapping.txt.
func isSymbolUploadFile(symbolType, name string) bool {
	if symbolType == typeAndroid {
		return filepath.Base(name) == androidMappingFileName
	}
	return isReactNativeUploadFile(name)
}

func getAllSymbolFiles(path, symbolType string) ([]SymbolFile, error) {
	var files []SymbolFile

	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !fileInfo.IsDir() {
		// Validate the explicit file against --type too, so a single --path can't
		// upload an unrelated file under this type's symbol keys.
		if !isSymbolUploadFile(symbolType, path) {
			return nil, unexpectedSymbolFileError(path, symbolType)
		}
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

		if !d.IsDir() && isSymbolUploadFile(symbolType, filePath) {
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
		if symbolType == typeAndroid {
			return nil, fmt.Errorf("no Android symbol files found (looked for %s). Please double check that R8/ProGuard produced a mapping file", androidMappingFileName)
		}
		return nil, fmt.Errorf("no React Native symbol files found (looked for *.jsbundle, *.jsbundle.map, *.bundle, *.bundle.map). Please double check that you have generated sourcemaps for your app")
	}

	return files, nil
}

// unexpectedSymbolFileError reports that an explicit --path file doesn't match
// the artifacts expected for the given --type.
func unexpectedSymbolFileError(path, symbolType string) error {
	if symbolType == typeAndroid {
		return fmt.Errorf("file %s is not an Android symbol file (expected %s)", path, androidMappingFileName)
	}
	return fmt.Errorf("file %s is not a React Native symbol file (expected *.jsbundle, *.jsbundle.map, *.bundle, *.bundle.map)", path)
}

func getS3Key(symbolsIDPrefix, symbolsID, version, basePath, fileName string) string {
	// Symbols Id Lane: a symbols id fully addresses the artifact, so it supersedes
	// the version+basePath scheme. The key becomes <prefix>/<symbolsID>/<basename>
	// so it matches the key the backend derives from the reported symbols id.
	if symbolsID != "" {
		return fmt.Sprintf("%s/%s/%s", symbolsIDPrefix, symbolsID, filepath.Base(fileName))
	}

	if version == "" {
		version = "unversioned"
	}

	if basePath != "" && !strings.HasSuffix(basePath, "/") {
		basePath = basePath + "/"
	}

	return fmt.Sprintf("%s/%s%s", version, basePath, fileName)
}

// symbolsIDForArtifact resolves the symbols id for one uploaded artifact from a
// *.symbolsid sidecar. A React Native build's bundle and its .map share a single
// id, but the Metro plugin writes only one sidecar (named after the source map
// it's handed). So for a .map we also check the bundle's sidecar and for a
// bundle we also check the .map's sidecar — keeping both files on the same lane
// instead of splitting one to the Version Lane. Returns "" when none is found.
func symbolsIDForArtifact(filePath string) string {
	candidates := []string{filePath + symbolsIDSidecarSuffix}
	if strings.HasSuffix(filePath, ".map") {
		candidates = append(candidates, strings.TrimSuffix(filePath, ".map")+symbolsIDSidecarSuffix)
	} else {
		candidates = append(candidates, filePath+".map"+symbolsIDSidecarSuffix)
	}
	for _, candidate := range candidates {
		if id := readSymbolsIDFile(candidate); id != "" {
			return id
		}
	}
	return ""
}

// readSymbolsIDFile returns the symbols id recorded in a *.symbolsid sidecar
// (the Metro plugin writes it next to the composed source map; the Android
// Gradle task writes mapping.txt.symbolsid). Best-effort: any error, or no
// sidecar, yields "" so the caller falls back to the Version Lane addressing.
func readSymbolsIDFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
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

	if len(urlsResp.Errors) > 0 {
		messages := make([]string, 0, len(urlsResp.Errors))
		for _, e := range urlsResp.Errors {
			messages = append(messages, e.Message)
		}
		return nil, fmt.Errorf("unable to generate symbol upload urls: %s", strings.Join(messages, "; "))
	}

	if len(urlsResp.Data.GetSymbolUploadUrls) == 0 {
		return nil, fmt.Errorf("unable to generate symbol upload urls: server returned no urls for %d path(s)", len(paths))
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
	cmd.Flags().String(typeFlag, "", fmt.Sprintf("The symbol type to upload (supported: %s, %s, %s)", typeReactNative, typeAndroid, typeAppleDSYM))
	_ = cmd.MarkFlagRequired(typeFlag)
	_ = cmd.Flags().SetAnnotation(typeFlag, "required", []string{"true"})
	_ = viper.BindPFlag(typeFlag, cmd.Flags().Lookup(typeFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(appVersionFlag, "", "The current version of your deploy")
	_ = viper.BindPFlag(appVersionFlag, cmd.Flags().Lookup(appVersionFlag))

	cmd.Flags().String(symbolsIdFlag, "", "The symbols id (launchdarkly.symbols_id.htlhash) to key uploads by (Symbols Id Lane). If omitted, a *.symbolsid sidecar next to the bundle is used when present")
	_ = viper.BindPFlag(symbolsIdFlag, cmd.Flags().Lookup(symbolsIdFlag))

	cmd.Flags().String(pathFlag, defaultPath, "Sets the directory of where the symbol files are")
	_ = viper.BindPFlag(pathFlag, cmd.Flags().Lookup(pathFlag))

	cmd.Flags().String(basePathFlag, "", "An optional base path for the uploaded symbol files")
	_ = viper.BindPFlag(basePathFlag, cmd.Flags().Lookup(basePathFlag))

	cmd.Flags().String(backendUrlFlag, defaultBackendUrl, "An optional backend url for self-hosted deployments")
	_ = viper.BindPFlag(backendUrlFlag, cmd.Flags().Lookup(backendUrlFlag))
}

package symbols

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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

	// includeSourcesFlag opts into uploading source files (as a .srcbundle beside
	// the symbol map) so the errors page can show source context around native
	// frames. Off by default: it ships your source to LaunchDarkly. Supported for
	// apple-dsym (sources come from the dSYM's DWARF) and android (sources are
	// scanned from --source-path, since an R8 mapping records no paths).
	includeSourcesFlag = "include-sources"

	// noSkipExistingFlag forces a re-upload of symbols LaunchDarkly already has.
	// Dedup is on by default and only applies to symbols-id (content addressed)
	// keys, so this is for repairing a corrupt stored object, not normal use.
	noSkipExistingFlag = "no-skip-existing"

	// sourcePathFlag is the directory scanned for .java/.kt sources when
	// --include-sources is used with --type android. It defaults to the current
	// directory because --path points at the mapping.txt output dir, which holds
	// no sources.
	sourcePathFlag = "source-path"

	defaultPath = "."

	// defaultBackendUrl is the observability API for LaunchDarkly production, which
	// is what defaultBackendURLFor derives for the default base URI.
	defaultBackendUrl = "https://pri.observability.app.launchdarkly.com"

	// Every LaunchDarkly instance publishes the observability API under its own
	// host, named for the instance the app is served from: staging's app at
	// ld-stg.launchdarkly.com has its API at pri.observability.ld-stg.launchdarkly.com.
	// "pri" is the authenticated graph, which is the one that hands out upload URLs.
	launchDarklyDomain     = "launchdarkly.com"
	observabilityAPIPrefix = "pri.observability."

	// reactNativeSymbolsIDPrefix is the storage "version" segment for symbols-id
	// addressed JS maps (Symbols Id Lane). Keys become _sym/js/id/<symbolsID>/<file>,
	// matching what the symbolication backend derives from the reported symbols id.
	reactNativeSymbolsIDPrefix = "_sym/js/id"

	// androidSymbolsIDPrefix is the equivalent Symbols Id Lane segment for Android
	// builds. Keys become _sym/android/id/<symbolsID>/mapping.v1.index.
	androidSymbolsIDPrefix = "_sym/android/id"

	// symbolsIDSidecarSuffix names the file written next to an artifact to record
	// its symbols id (the Metro plugin for React Native, the Gradle task for
	// Android), so `ldcli` can upload with the exact id the app reports without a
	// manual --symbols-id.
	symbolsIDSidecarSuffix = ".symbolsid"

	// androidMappingFileName is the R8/ProGuard mapping `ldcli` discovers for
	// --type android and indexes. The mapping itself is not uploaded; see
	// android_upload.go.
	androidMappingFileName = "mapping.txt"

	// typeReactNative uploads React Native Hermes/Metro sourcemaps (ordinary
	// JavaScript sourcemaps).
	typeReactNative = "react-native"

	// typeAndroid indexes an Android R8/ProGuard `mapping.txt` and uploads the
	// index, for Java/Kotlin stack-trace retrace.
	typeAndroid = "android"

	// typeAppleDSYM compiles Apple dSYM debug info into per-architecture .dsymmap
	// symbol maps (keyed by build UUID) for iOS/macOS crash symbolication. It is
	// the canonical value; see symbolTypeAliases for accepted synonyms.
	typeAppleDSYM = "apple-dsym"

	// typeFlutter compiles Flutter/Dart AOT debug symbols (app.<platform>.symbols)
	// into per-build .dartmap symbol maps (keyed by the Dart snapshot build id,
	// surfaced as symbols_id) for obfuscated Dart crash symbolication.
	typeFlutter = "flutter"

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

	// getSymbolUrlsDedupQuery asks the backend to answer keys it already stores with
	// an empty string. A separate document because a backend that predates these
	// arguments rejects the whole query, which getSymbolUploadUrls falls back from.
	getSymbolUrlsDedupQuery = `
	  query GetSymbolUploadUrls($api_key: String!, $project_id: String!, $paths: [String!]!, $skip_existing: Boolean, $digests: [String!]) {
	    get_symbol_upload_urls_ld(
			api_key: $api_key
			project_id: $project_id
			paths: $paths
			skip_existing: $skip_existing
			digests: $digests
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
		symbolType := canonicalizeSymbolType(viper.GetString(typeFlag))
		if !isSupportedType(symbolType) {
			return fmt.Errorf("unsupported --type %q; supported types: %s, %s, %s, %s", viper.GetString(typeFlag), typeReactNative, typeAndroid, typeAppleDSYM, typeFlutter)
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
		// Dedup is the default; --no-skip-existing forces every byte to be resent.
		skipExisting := !viper.GetBool(noSkipExistingFlag)

		if backendUrl == "" {
			backendUrl = defaultBackendURLFor(viper.GetString(cliflags.BaseURIFlag))
		}

		// Apple dSYMs take a dedicated path: they are compiled to per-arch .dsymmap
		// symbol maps keyed by build UUID, ignoring the version/symbols-id lanes.
		if symbolType == typeAppleDSYM {
			// A dSYM is best uploaded by the build that produced it, so where to
			// read one from can come from the build itself. See apple_xcode.go.
			upload := resolveAppleUpload(path)
			fmt.Printf("Starting to upload %s symbols from %s\n", symbolType, upload.Path)
			return uploadAppleDSYMs(viper.GetString(cliflags.AccessTokenFlag), projectResult.ID, upload, backendUrl, viper.GetBool(includeSourcesFlag), skipExisting)
		}

		// Flutter/Dart symbols take a dedicated path too: each app.<platform>.symbols
		// is compiled to a .dartmap keyed by its build id (Id Lane), plus a
		// Version-lane copy when --app-version is set.
		if symbolType == typeFlutter {
			fmt.Printf("Starting to upload %s symbols from %s\n", symbolType, path)
			return uploadFlutterSymbols(viper.GetString(cliflags.AccessTokenFlag), projectResult.ID, path, appVersion, backendUrl, viper.GetBool(includeSourcesFlag), viper.GetString(sourcePathFlag), skipExisting)
		}

		// Android takes a dedicated path as well: the R8 mapping is compiled into the
		// index symbolication reads, and stored under the lanes a crash can arrive on.
		if symbolType == typeAndroid {
			return uploadAndroidSymbols(viper.GetString(cliflags.AccessTokenFlag), projectResult.ID, path, appVersion, symbolsID, backendUrl, viper.GetBool(includeSourcesFlag), viper.GetString(sourcePathFlag), skipExisting)
		}

		fmt.Printf("Starting to upload %s symbols from %s\n", symbolType, path)
		if symbolsID != "" {
			fmt.Printf("Using symbols id %s for all files (Symbols Id Lane: %s/%s)\n", symbolsID, reactNativeSymbolsIDPrefix, symbolsID)
		}

		files, err := getAllSymbolFiles(path, symbolType)
		if err != nil {
			return fmt.Errorf("failed to find symbol files: %w", err)
		}

		if len(files) == 0 {
			return fmt.Errorf("no symbol files found in %s, is this the correct path?", path)
		}

		// Symbols Id Lane: resolve the id per file so one upload of a build's bundle
		// and its map keys both by the id the app reports. An explicit --symbols-id
		// overrides all files; otherwise each artifact's *.symbolsid sidecar (or its
		// sibling's — see symbolsIDForArtifact) is used, falling back to the Version
		// Lane (version+basePath) when there is none.
		s3Keys := make([]string, 0, len(files))
		for _, file := range files {
			fileSymbolsID := symbolsID
			if fileSymbolsID == "" {
				fileSymbolsID = symbolsIDForArtifact(file.Path)
				if fileSymbolsID != "" {
					fmt.Printf("Using symbols id %s for %s (Symbols Id Lane: %s/%s)\n", fileSymbolsID, file.Name, reactNativeSymbolsIDPrefix, fileSymbolsID)
				}
			}
			s3Keys = append(s3Keys, getS3Key(reactNativeSymbolsIDPrefix, fileSymbolsID, appVersion, basePath, file.Name))
		}

		// No digests: a JavaScript map is keyed either by its own content id or by a
		// Version Lane key the backend re-presigns so it can overwrite, so nothing
		// here needs a hash to settle whether it is already stored.
		uploadUrls, err := getSymbolUploadUrls(viper.GetString(cliflags.AccessTokenFlag), projectResult.ID, s3Keys, nil, backendUrl, skipExisting)
		if err != nil {
			return fmt.Errorf("failed to get upload URLs: %w", err)
		}

		// The loop below pairs each requested key with uploadUrls[i], so a short
		// list would panic. Require one URL per requested key.
		if len(uploadUrls) != len(s3Keys) {
			return fmt.Errorf("expected %d upload URLs but received %d", len(s3Keys), len(uploadUrls))
		}

		skipped := 0
		for i, file := range files {
			if alreadyUploaded(uploadUrls[i]) {
				fmt.Printf("Skipping %s, already uploaded\n", file.Name)
				skipped++
				continue
			}
			if err := uploadFile(file.Path, uploadUrls[i], file.Name); err != nil {
				return fmt.Errorf("failed to upload file %s: %w", file.Path, err)
			}
		}

		reportUploadSummary(skipped)
		return nil
	}
}

// alreadyUploaded reports whether the backend answered a key with "skip" rather
// than a URL. The empty string is the signal, which is why asking for it is opt-in.
func alreadyUploaded(uploadURL string) bool {
	return uploadURL == ""
}

// reportUploadSummary closes out an upload, and when anything was skipped says so on
// stderr as well.
//
// Skipping is new behavior, and someone re-uploading to repair a stored object would
// otherwise read "Successfully uploaded" from a run that sent nothing. The notice is
// transitional and can come out once a release or two has gone by; the counts stay on
// stdout so scripts parsing them are unaffected.
func reportUploadSummary(skipped int) {
	if skipped == 0 {
		fmt.Println("Successfully uploaded all symbols")
		return
	}

	fmt.Printf("Successfully uploaded all symbols (%d already present)\n", skipped)
	fmt.Fprintf(os.Stderr,
		"Note: %d file(s) were skipped because LaunchDarkly already stores them under the same content-derived id. This is new in this release; re-run with --%s to upload them anyway.\n",
		skipped, noSkipExistingFlag,
	)
}

// symbolTypeAliases maps user-friendly synonyms to a canonical --type value.
// All Apple platforms share the single dSYM-based pipeline, so any Apple
// platform acronym resolves to apple-dsym.
var symbolTypeAliases = map[string]string{
	"apple":      typeAppleDSYM,
	"apple-dsym": typeAppleDSYM,
	"dsym":       typeAppleDSYM,
	"ios":        typeAppleDSYM,
	"ipados":     typeAppleDSYM,
	"tvos":       typeAppleDSYM,
	"watchos":    typeAppleDSYM,
	"visionos":   typeAppleDSYM,
	"macos":      typeAppleDSYM,
	"osx":        typeAppleDSYM,
	"flutter":    typeFlutter,
	"dart":       typeFlutter,
}

// canonicalizeSymbolType resolves a user-supplied --type to its canonical value.
// Matching is case-insensitive and understands platform synonyms (e.g. "ios",
// "macos" -> apple-dsym). Unknown values are returned lower-cased/trimmed so
// isSupportedType can reject them with a clear error.
func canonicalizeSymbolType(symbolType string) string {
	s := strings.ToLower(strings.TrimSpace(symbolType))
	if canonical, ok := symbolTypeAliases[s]; ok {
		return canonical
	}
	return s
}

func isSupportedType(symbolType string) bool {
	return symbolType == typeReactNative || symbolType == typeAndroid || symbolType == typeAppleDSYM || symbolType == typeFlutter
}

func isReactNativeUploadFile(name string) bool {
	for _, suffix := range reactNativeUploadSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// isSymbolUploadFile reports whether a discovered file is an input for the given
// symbol type: React Native bundles and maps, or an Android mapping.txt.
func isSymbolUploadFile(symbolType, name string) bool {
	if symbolType == typeAndroid {
		return filepath.Base(name) == androidMappingFileName
	}
	return isReactNativeUploadFile(name)
}

// uploadName is the name an artifact is stored under, given its path relative to
// the directory searched.
func uploadName(symbolType, relPath string) string {
	if symbolType == typeAndroid {
		return filepath.Base(relPath)
	}
	return relPath
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
				// An Android mapping is named for the file alone however deep it was
				// found, since what it is stored as is decided by the indexer. A
				// React Native bundle keeps its path, which is part of how a map is
				// matched to the bundle that references it.
				Name: uploadName(symbolType, relPath),
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

// defaultBackendURLFor derives the observability API endpoint from the LaunchDarkly
// base URI, so aiming the CLI at another instance takes the one flag that names the
// instance rather than two flags that have to agree.
//
// Only LaunchDarkly's own hosts are derived from. A base URI pointing at a local or
// proxied stack says nothing about where its observability API listens, so those keep
// the production default and --backend-url stays the way to say otherwise.
func defaultBackendURLFor(baseURI string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURI))
	if err != nil {
		return defaultBackendUrl
	}

	host := parsed.Hostname()
	if host != launchDarklyDomain && !strings.HasSuffix(host, "."+launchDarklyDomain) {
		return defaultBackendUrl
	}
	return "https://" + observabilityAPIPrefix + host
}

// getSymbolUploadUrls returns one upload URL per requested key, in order.
//
// With skipExisting, a key whose bytes the backend already stores comes back empty
// and callers must skip it. digests is parallel to paths and may be nil or hold ""
// for a key the caller has no digest for; it is what lets the backend settle a key
// that isn't derived from its own contents, since there existence proves nothing.
//
// A backend that predates these arguments rejects the query, so this retries once
// without them, keeping an updated CLI working against an older deployment.
func getSymbolUploadUrls(apiKey, projectID string, paths, digests []string, backendUrl string, skipExisting bool) ([]string, error) {
	urls, err := requestSymbolUploadUrls(apiKey, projectID, paths, digests, backendUrl, skipExisting)
	if err != nil && skipExisting && mentionsDedupArgument(err) {
		return requestSymbolUploadUrls(apiKey, projectID, paths, nil, backendUrl, false)
	}
	return urls, err
}

// The dedup arguments, named in both the request and the validation error a backend
// without them returns.
const (
	skipExistingArgument = "skip_existing"
	digestsArgument      = "digests"
)

// mentionsDedupArgument reports whether err is a backend rejecting an argument it
// doesn't know. Either name means the deployment predates dedup, since both arrived
// together; validation names whichever it reached.
func mentionsDedupArgument(err error) bool {
	return strings.Contains(err.Error(), skipExistingArgument) ||
		strings.Contains(err.Error(), digestsArgument)
}

// contentDigest is the hex MD5 of bytes about to be uploaded, which is what S3
// reports as the ETag of an object stored in one part. Sending it lets the backend
// prove that what it already stores under a key is exactly these bytes.
func contentDigest(data []byte) string {
	return fmt.Sprintf("%x", md5.Sum(data))
}

func requestSymbolUploadUrls(apiKey, projectID string, paths, digests []string, backendUrl string, skipExisting bool) ([]string, error) {
	variables := map[string]interface{}{
		"api_key":    apiKey,
		"project_id": projectID,
		"paths":      paths,
	}
	query := getSymbolUrlsQuery
	if skipExisting {
		query = getSymbolUrlsDedupQuery
		variables[skipExistingArgument] = true
		// The backend wants one digest per path or none, so an all-empty slice is
		// left off entirely rather than sent as padding.
		if slices.ContainsFunc(digests, func(d string) bool { return d != "" }) {
			variables[digestsArgument] = digests
		}
	}

	reqBody, err := json.Marshal(map[string]interface{}{
		"query":     query,
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

// uploadFile sends a file on disk, gzipped. Nothing keyed to a file carries a
// digest — those keys are derived from the artifact's own contents — so unlike an
// artifact held in memory this can be compressed here, as it is read.
func uploadFile(filePath, uploadUrl, name string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	compressed, err := gzipFile(filePath)
	if err != nil {
		return err
	}

	// Compressing an artifact that is already compressed can make it bigger; send
	// the file as it is rather than pay to store the difference.
	if int64(len(compressed)) >= info.Size() {
		file, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer file.Close()

		if err := putObject(uploadUrl, file, info.Size(), ""); err != nil {
			return err
		}
		fmt.Printf("[LaunchDarkly] Uploaded %s to %s (%s)\n", filePath, name, byteSize(info.Size()))
		return nil
	}

	if err := putObject(uploadUrl, bytes.NewReader(compressed), int64(len(compressed)), gzipEncoding); err != nil {
		return err
	}
	fmt.Printf("[LaunchDarkly] Uploaded %s to %s (%s gzipped to %s)\n",
		filePath, name, byteSize(info.Size()), byteSize(int64(len(compressed))))
	return nil
}

// putObject PUTs a body to a presigned URL.
//
// The length is passed explicitly because S3 will not take a chunked upload, and
// net/http only measures a body it recognises — a file streamed from disk would
// otherwise be sent with no Content-Length at all.
func putObject(uploadURL string, body io.Reader, length int64, encoding string) error {
	req, err := http.NewRequest("PUT", uploadURL, body)
	if err != nil {
		return err
	}
	req.ContentLength = length
	if encoding != "" {
		// Stored as object metadata and handed back on read, so the object says how
		// to read it instead of the reader having to guess.
		req.Header.Set("Content-Encoding", encoding)
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
	return nil
}

func initFlags(cmd *cobra.Command) {
	cmd.Flags().String(typeFlag, "", fmt.Sprintf("The symbol type to upload (supported: %s, %s, %s, %s; %s also accepts ios/ipados/tvos/watchos/visionos/macos/apple/dsym; %s also accepts dart)", typeReactNative, typeAndroid, typeAppleDSYM, typeFlutter, typeAppleDSYM, typeFlutter))
	_ = cmd.MarkFlagRequired(typeFlag)
	_ = cmd.Flags().SetAnnotation(typeFlag, "required", []string{"true"})
	_ = viper.BindPFlag(typeFlag, cmd.Flags().Lookup(typeFlag))

	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(appVersionFlag, "", fmt.Sprintf("The current version of your deploy. With --type %s this is read from the packaged build when omitted", typeAndroid))
	_ = viper.BindPFlag(appVersionFlag, cmd.Flags().Lookup(appVersionFlag))

	cmd.Flags().String(symbolsIdFlag, "", fmt.Sprintf("The symbols id (launchdarkly.symbols_id.htlhash) to key uploads by (Symbols Id Lane). If omitted, a *.symbolsid sidecar next to the bundle is used when present, and with --type %s the id the packaged app reports, or failing that the one R8 recorded in the mapping", typeAndroid))
	_ = viper.BindPFlag(symbolsIdFlag, cmd.Flags().Lookup(symbolsIdFlag))

	cmd.Flags().String(pathFlag, defaultPath, fmt.Sprintf("Sets the directory of where the symbol files are. With --type %s, run from your project root and the R8 mapping is found for you; with --type %s, an Xcode build phase uploads what it just built", typeAndroid, typeAppleDSYM))
	_ = viper.BindPFlag(pathFlag, cmd.Flags().Lookup(pathFlag))

	cmd.Flags().String(basePathFlag, "", "An optional base path for the uploaded symbol files")
	_ = viper.BindPFlag(basePathFlag, cmd.Flags().Lookup(basePathFlag))

	cmd.Flags().String(backendUrlFlag, "", fmt.Sprintf("An optional backend url for self-hosted deployments. Defaults to the observability API of whichever instance --%s names (%s for the default)", cliflags.BaseURIFlag, defaultBackendUrl))
	_ = viper.BindPFlag(backendUrlFlag, cmd.Flags().Lookup(backendUrlFlag))

	cmd.Flags().Bool(includeSourcesFlag, false, fmt.Sprintf("Also upload your source files so the errors page can show source context around native frames (%s, %s, and %s). Your source is stored in LaunchDarkly", typeAppleDSYM, typeAndroid, typeFlutter))
	_ = viper.BindPFlag(includeSourcesFlag, cmd.Flags().Lookup(includeSourcesFlag))

	cmd.Flags().Bool(noSkipExistingFlag, false, "Re-upload symbols even when LaunchDarkly already has them. By default a symbols-id file that is already stored is skipped, since its id is derived from its contents")
	_ = viper.BindPFlag(noSkipExistingFlag, cmd.Flags().Lookup(noSkipExistingFlag))

	cmd.Flags().String(sourcePathFlag, defaultPath, fmt.Sprintf("Directory to resolve your sources from when using --%s: the tree to scan for .java/.kt with --type %s, or your Flutter project root (the directory holding pubspec.yaml, which names the package your .dart files are compiled under) with --type %s", includeSourcesFlag, typeAndroid, typeFlutter))
	_ = viper.BindPFlag(sourcePathFlag, cmd.Flags().Lookup(sourcePathFlag))
}

package symbols

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/analytics"
)

const (
	// outputFlag is "out" rather than "output" to avoid colliding with the
	// global --output flag that selects the CLI output format.
	outputFlag = "out"

	// defaultOutput is where generated symbol files are written when --out is
	// omitted.
	defaultOutput = "symbols"
)

// NewGenerateCmd builds `symbols generate`, which runs the same symbol
// processing as `symbols upload` but writes the resulting files to a local
// folder instead of uploading them. The output folder mirrors the storage key
// layout the backend expects, so it can be inspected or uploaded later by other
// means.
func NewGenerateCmd(analyticsTrackerFn analytics.TrackerFn) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Use:   "generate",
		Short: "Generate symbol files to a local folder",
		Long:  "Generate symbol files (React Native sourcemaps, Android R8/ProGuard mappings, or Apple dSYMs) into a local folder instead of uploading them to LaunchDarkly. The folder mirrors the storage layout the symbolication backend expects.",
		RunE:  generateRunE(),
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
	initGenerateFlags(cmd)

	return cmd
}

func generateRunE() func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		symbolType := canonicalizeSymbolType(viper.GetString(typeFlag))
		if !isSupportedType(symbolType) {
			return fmt.Errorf("unsupported --type %q; supported types: %s, %s, %s, %s", viper.GetString(typeFlag), typeReactNative, typeAndroid, typeAppleDSYM, typeFlutter)
		}

		path := viper.GetString(pathFlag)
		outputDir := viper.GetString(outputFlag)
		if outputDir == "" {
			outputDir = defaultOutput
		}

		fmt.Printf("Generating %s symbols from %s into %s\n", symbolType, path, outputDir)

		// Apple dSYMs are compiled into per-arch .dsymmap symbol maps keyed by build
		// UUID, ignoring the version/symbols-id lanes.
		if symbolType == typeAppleDSYM {
			return generateAppleDSYMs(path, outputDir)
		}

		// Flutter symbols compile to .dartmap maps keyed by build id (Id Lane),
		// plus a Version-lane copy when --app-version is set.
		if symbolType == typeFlutter {
			return generateFlutterSymbols(path, viper.GetString(appVersionFlag), outputDir)
		}

		return generateSymbolFiles(symbolType, path, outputDir)
	}
}

// generateAppleDSYMs compiles the discovered dSYM images to .dsymmap symbol maps
// and writes one file per build UUID under outputDir, using the same storage
// key (_sym/apple/id/<UUID>.dsymmap) that `symbols upload` would use.
func generateAppleDSYMs(path, outputDir string) error {
	images, err := findDSYMImages(path)
	if err != nil {
		return fmt.Errorf("failed to find dSYM files: %w", err)
	}
	if len(images) == 0 {
		return fmt.Errorf("no .dSYM bundles found in %s, is this the correct path?", path)
	}

	maps, err := buildAppleMaps(images)
	if err != nil {
		return err
	}
	if len(maps) == 0 {
		return fmt.Errorf("no architectures found in the discovered dSYM files")
	}

	for _, m := range maps {
		if err := writeSymbolFile(outputDir, m.Key, m.Data); err != nil {
			return fmt.Errorf("failed to write symbol map for %s: %w", m.UUID, err)
		}
	}

	fmt.Printf("Successfully generated %d symbol file(s) in %s\n", len(maps), outputDir)
	return nil
}

// generateFlutterSymbols compiles the discovered app.*.symbols to .dartmap
// symbol maps and writes them under outputDir using the same storage keys
// `symbols upload` would use (Id lane, plus Version lane when appVersion is set).
func generateFlutterSymbols(path, appVersion, outputDir string) error {
	uploads, err := buildFlutterMaps(path, appVersion)
	if err != nil {
		return err
	}
	for _, u := range uploads {
		if err := writeSymbolFile(outputDir, u.Key, u.Data); err != nil {
			return fmt.Errorf("failed to write symbol map %s: %w", u.Label, err)
		}
	}
	fmt.Printf("Successfully generated %d symbol file(s) in %s\n", len(uploads), outputDir)
	return nil
}

// generateSymbolFiles discovers React Native or Android artifacts and copies
// each one to outputDir under the same storage key `symbols upload` would use,
// so the generated folder matches what the backend expects.
func generateSymbolFiles(symbolType, path, outputDir string) error {
	files, err := getAllSymbolFiles(path, symbolType)
	if err != nil {
		return fmt.Errorf("failed to find symbol files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no symbol files found in %s, is this the correct path?", path)
	}

	symbolsID := viper.GetString(symbolsIdFlag)
	appVersion := viper.GetString(appVersionFlag)
	basePath := viper.GetString(basePathFlag)
	symbolsIDPrefix := symbolsIDPrefixForType(symbolType)

	for _, file := range files {
		fileSymbolsID := symbolsID
		if fileSymbolsID == "" {
			fileSymbolsID = symbolsIDForArtifact(file.Path)
		}
		key := getS3Key(symbolsIDPrefix, fileSymbolsID, appVersion, basePath, file.Name)

		data, err := os.ReadFile(file.Path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file.Path, err)
		}
		if err := writeSymbolFile(outputDir, key, data); err != nil {
			return fmt.Errorf("failed to write %s: %w", file.Name, err)
		}
	}

	fmt.Printf("Successfully generated %d symbol file(s) in %s\n", len(files), outputDir)
	return nil
}

// writeSymbolFile writes data to outputDir/key, creating parent directories as
// needed. key uses forward slashes (a storage key); filepath.FromSlash maps it
// to the host separator so nested keys become nested folders.
func writeSymbolFile(outputDir, key string, data []byte) error {
	dest := filepath.Join(outputDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("[LaunchDarkly] Wrote %s\n", dest)
	return nil
}

func initGenerateFlags(cmd *cobra.Command) {
	cmd.Flags().String(typeFlag, "", fmt.Sprintf("The symbol type to generate (supported: %s, %s, %s, %s; %s also accepts ios/ipados/tvos/watchos/visionos/macos/apple/dsym; %s also accepts dart)", typeReactNative, typeAndroid, typeAppleDSYM, typeFlutter, typeAppleDSYM, typeFlutter))
	_ = cmd.MarkFlagRequired(typeFlag)
	_ = cmd.Flags().SetAnnotation(typeFlag, "required", []string{"true"})
	_ = viper.BindPFlag(typeFlag, cmd.Flags().Lookup(typeFlag))

	cmd.Flags().String(pathFlag, defaultPath, "Sets the directory of where the symbol files are")
	_ = viper.BindPFlag(pathFlag, cmd.Flags().Lookup(pathFlag))

	cmd.Flags().String(outputFlag, defaultOutput, "The directory to write the generated symbol files to (default: symbols)")
	_ = viper.BindPFlag(outputFlag, cmd.Flags().Lookup(outputFlag))

	cmd.Flags().String(appVersionFlag, "", "The current version of your deploy")
	_ = viper.BindPFlag(appVersionFlag, cmd.Flags().Lookup(appVersionFlag))

	cmd.Flags().String(symbolsIdFlag, "", "The symbols id (launchdarkly.symbols_id.htlhash) to key files by (Symbols Id Lane). If omitted, a *.symbolsid sidecar next to the bundle is used when present")
	_ = viper.BindPFlag(symbolsIdFlag, cmd.Flags().Lookup(symbolsIdFlag))

	cmd.Flags().String(basePathFlag, "", "An optional base path for the generated symbol files")
	_ = viper.BindPFlag(basePathFlag, cmd.Flags().Lookup(basePathFlag))
}

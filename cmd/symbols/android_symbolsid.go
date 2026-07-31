package symbols

import (
	"archive/zip"
	"io"
	"regexp"
	"strings"
)

// The symbols id an Android build reports.
//
// A release build stamps a content-derived id of its mapping into
// assets/ld_symbols_id.txt, and the SDK reports it as
// `launchdarkly.symbols_id.htlhash` so symbolication can find the mapping by
// content instead of by app version. Uploading on that lane means keying the
// object by the same id.
//
// The id is read out of the packaged app rather than from a file the build staged
// beside the mapping, because the packaged app is the thing that will run: what it
// carries is exactly what will be reported. That leaves nothing for a build script
// to hand over, and no way for the two to disagree.

const (
	// androidSymbolsIDAsset is the asset the build writes the id to, packaged at
	// assets/ in an APK and base/assets/ in an app bundle.
	androidSymbolsIDAsset = "ld_symbols_id.txt"

	apkSymbolsIDEntry = "assets/" + androidSymbolsIDAsset
	aabSymbolsIDEntry = "base/assets/" + androidSymbolsIDAsset

	// symbolsIDAssetMaxBytes caps how much of the asset is read. The id is 32
	// bytes; anything remotely larger is not one, and is not worth reading.
	symbolsIDAssetMaxBytes = 128
)

// symbolsIDPattern is the shape of a symbols id: the first 16 bytes of a SHA-256
// as lowercase hex. It matches the check the SDK applies before reporting one, so
// an id the app would discard is not uploaded to a lane nothing will ask for.
var symbolsIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// SymbolsID returns the symbols id the variant's packaged app will report, or ""
// when the build does not stamp one — in which case the mapping belongs on the
// Version Lane, as before.
func (b *androidBuild) SymbolsID() string {
	for _, app := range b.packagedApps() {
		if app.Path == "" {
			continue
		}
		if id := symbolsIDFromPackagedApp(app.Path); id != "" {
			return id
		}
	}
	return ""
}

// symbolsIDFromPackagedApp reads the stamped id out of an APK or AAB, both of
// which are zips. Best-effort: an app that stamps no id, or a file that cannot be
// read as one, yields "" so the upload falls back to the Version Lane rather than
// failing a build over it.
func symbolsIDFromPackagedApp(path string) string {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer archive.Close()

	for _, file := range archive.File {
		if file.Name != apkSymbolsIDEntry && file.Name != aabSymbolsIDEntry {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			return ""
		}
		content, err := io.ReadAll(io.LimitReader(reader, symbolsIDAssetMaxBytes))
		_ = reader.Close()
		if err != nil {
			return ""
		}
		id := strings.TrimSpace(string(content))
		if symbolsIDPattern.MatchString(id) {
			return id
		}
		return ""
	}
	return ""
}

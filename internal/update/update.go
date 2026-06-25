package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/launchdarkly/ldcli/internal/config"
)

const (
	defaultGitHubReleasesURL = "https://api.github.com/repos/launchdarkly/ldcli/releases/latest"
	cacheTTL                 = 24 * time.Hour
	errorCacheTTL            = 1 * time.Hour
	cacheFilename            = "update-check.json"
	httpTimeout              = 3 * time.Second
)

var releasesURL = defaultGitHubReleasesURL

type UpdateInfo struct {
	CurrentVersion string
	LatestVersion  string
	IsNewer        bool
}

type cacheEntry struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

// cacheFilePathFn is a function variable to allow test overrides.
var cacheFilePathFn = defaultCacheFilePath

func defaultCacheFilePath() string {
	configFile := config.GetConfigFile()
	return filepath.Join(filepath.Dir(configFile), cacheFilename)
}

func readCache() (*cacheEntry, error) {
	data, err := os.ReadFile(cacheFilePathFn())
	if err != nil {
		return nil, err
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func writeCache(entry *cacheEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	_ = os.MkdirAll(filepath.Dir(cacheFilePathFn()), os.ModePerm)
	_ = os.WriteFile(cacheFilePathFn(), data, 0644)
}

func fetchLatestVersion(client *http.Client) (string, error) {
	req, err := http.NewRequest(http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return "", err
	}

	return normalizeVersion(release.TagName), nil
}

// normalizeVersion strips a leading "v" prefix if present.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

// compareVersions returns true if latest is newer than current.
// Uses a simple split-and-compare on major.minor.patch integers.
func compareVersions(current, latest string) bool {
	parseParts := func(v string) [3]int {
		var parts [3]int
		n, _ := fmt.Sscanf(v, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
		if n < 1 {
			return [3]int{}
		}
		return parts
	}

	c := parseParts(current)
	l := parseParts(latest)

	for i := 0; i < 3; i++ {
		if l[i] > c[i] {
			return true
		}
		if l[i] < c[i] {
			return false
		}
	}

	return false
}

// CheckForUpdate checks GitHub for the latest release and compares it to the
// current version. It caches the result to avoid hitting the network on every
// invocation. Returns nil when no update is available or on any error.
func CheckForUpdate(currentVersion string) *UpdateInfo {
	currentVersion = normalizeVersion(currentVersion)
	if currentVersion == "dev" || currentVersion == "test" || currentVersion == "" {
		return nil
	}

	cached, err := readCache()
	if err == nil && time.Since(cached.CheckedAt) < cacheTTL {
		if !compareVersions(currentVersion, cached.LatestVersion) {
			return nil
		}
		return &UpdateInfo{
			CurrentVersion: currentVersion,
			LatestVersion:  cached.LatestVersion,
			IsNewer:        true,
		}
	}

	client := &http.Client{Timeout: httpTimeout}
	latest, err := fetchLatestVersion(client)
	if err != nil {
		writeCache(&cacheEntry{
			LatestVersion: currentVersion,
			CheckedAt:     time.Now().Add(errorCacheTTL - cacheTTL),
		})
		return nil
	}

	writeCache(&cacheEntry{
		LatestVersion: latest,
		CheckedAt:     time.Now(),
	})

	if !compareVersions(currentVersion, latest) {
		return nil
	}

	return &UpdateInfo{
		CurrentVersion: currentVersion,
		LatestVersion:  latest,
		IsNewer:        true,
	}
}

// NotificationMessage returns a user-facing string about the available update.
func NotificationMessage(info *UpdateInfo) string {
	return fmt.Sprintf(
		"\nA new version of ldcli is available: %s → %s\nhttps://github.com/launchdarkly/ldcli/releases/latest\n",
		info.CurrentVersion,
		info.LatestVersion,
	)
}

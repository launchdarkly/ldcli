package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeVersion(t *testing.T) {
	assert.Equal(t, "2.1.0", normalizeVersion("v2.1.0"))
	assert.Equal(t, "2.1.0", normalizeVersion("2.1.0"))
	assert.Equal(t, "", normalizeVersion(""))
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{"newer major", "1.0.0", "2.0.0", true},
		{"newer minor", "2.0.0", "2.1.0", true},
		{"newer patch", "2.1.0", "2.1.1", true},
		{"same version", "2.1.0", "2.1.0", false},
		{"older major", "3.0.0", "2.0.0", false},
		{"older minor", "2.2.0", "2.1.0", false},
		{"older patch", "2.1.2", "2.1.1", false},
		{"empty current", "", "2.1.0", true},
		{"empty latest", "2.1.0", "", false},
		{"both empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareVersions(tt.current, tt.latest)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFetchLatestVersion(t *testing.T) {
	t.Run("parses tag_name from GitHub response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "application/vnd.github.v3+json", r.Header.Get("Accept"))
			w.WriteHeader(http.StatusOK)
			resp := githubRelease{TagName: "v3.0.0"}
			data, _ := json.Marshal(resp)
			_, _ = w.Write(data)
		}))
		defer server.Close()

		origURL := releasesURL
		releasesURL = server.URL
		defer func() { releasesURL = origURL }()

		version, err := fetchLatestVersion(server.Client())
		require.NoError(t, err)
		assert.Equal(t, "3.0.0", version)
	})

	t.Run("handles non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		origURL := releasesURL
		releasesURL = server.URL
		defer func() { releasesURL = origURL }()

		_, err := fetchLatestVersion(server.Client())
		assert.Error(t, err)
	})
}

func TestReadWriteCache(t *testing.T) {
	tmpDir := t.TempDir()
	origFn := cacheFilePathFn
	cacheFilePathFn = func() string {
		return filepath.Join(tmpDir, cacheFilename)
	}
	defer func() { cacheFilePathFn = origFn }()

	_, err := readCache()
	assert.Error(t, err, "cache should not exist yet")

	now := time.Now().Truncate(time.Second)
	writeCache(&cacheEntry{
		LatestVersion: "3.0.0",
		CheckedAt:     now,
	})

	cached, err := readCache()
	require.NoError(t, err)
	assert.Equal(t, "3.0.0", cached.LatestVersion)
	assert.Equal(t, now.Unix(), cached.CheckedAt.Unix())
}

func TestCheckForUpdate_DevVersion(t *testing.T) {
	assert.Nil(t, CheckForUpdate("dev"))
	assert.Nil(t, CheckForUpdate("test"))
	assert.Nil(t, CheckForUpdate(""))
}

func TestCheckForUpdate_CachedNewerVersion(t *testing.T) {
	tmpDir := t.TempDir()
	origFn := cacheFilePathFn
	cacheFilePathFn = func() string {
		return filepath.Join(tmpDir, cacheFilename)
	}
	defer func() { cacheFilePathFn = origFn }()

	entry := cacheEntry{
		LatestVersion: "5.0.0",
		CheckedAt:     time.Now(),
	}
	data, _ := json.Marshal(entry)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, cacheFilename), data, 0644))

	info := CheckForUpdate("2.1.0")
	require.NotNil(t, info)
	assert.True(t, info.IsNewer)
	assert.Equal(t, "2.1.0", info.CurrentVersion)
	assert.Equal(t, "5.0.0", info.LatestVersion)
}

func TestCheckForUpdate_VPrefixCurrentVersion(t *testing.T) {
	tmpDir := t.TempDir()
	origFn := cacheFilePathFn
	cacheFilePathFn = func() string {
		return filepath.Join(tmpDir, cacheFilename)
	}
	defer func() { cacheFilePathFn = origFn }()

	entry := cacheEntry{
		LatestVersion: "2.1.0",
		CheckedAt:     time.Now(),
	}
	data, _ := json.Marshal(entry)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, cacheFilename), data, 0644))

	info := CheckForUpdate("v2.1.0")
	assert.Nil(t, info, "v-prefixed currentVersion matching latest should not report an update")
}

func TestCheckForUpdate_CachedSameVersion(t *testing.T) {
	tmpDir := t.TempDir()
	origFn := cacheFilePathFn
	cacheFilePathFn = func() string {
		return filepath.Join(tmpDir, cacheFilename)
	}
	defer func() { cacheFilePathFn = origFn }()

	entry := cacheEntry{
		LatestVersion: "2.1.0",
		CheckedAt:     time.Now(),
	}
	data, _ := json.Marshal(entry)
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, cacheFilename), data, 0644))

	info := CheckForUpdate("2.1.0")
	assert.Nil(t, info)
}

func TestNotificationMessage(t *testing.T) {
	info := &UpdateInfo{
		CurrentVersion: "2.1.0",
		LatestVersion:  "3.0.0",
		IsNewer:        true,
	}
	msg := NotificationMessage(info)
	assert.Contains(t, msg, "2.1.0")
	assert.Contains(t, msg, "3.0.0")
	assert.Contains(t, msg, "https://github.com/launchdarkly/ldcli/releases/latest")
}

func withCacheDir(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	origFn := cacheFilePathFn
	cacheFilePathFn = func() string {
		return filepath.Join(tmpDir, cacheFilename)
	}
	t.Cleanup(func() { cacheFilePathFn = origFn })
}

func withReleasesURL(t *testing.T, url string) {
	t.Helper()
	origURL := releasesURL
	releasesURL = url
	t.Cleanup(func() { releasesURL = origURL })
}

func TestCheckForUpdate_WritesBackoffCacheBeforeFetchCompletes(t *testing.T) {
	withCacheDir(t)

	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
		resp := githubRelease{TagName: "v9.0.0"}
		data, _ := json.Marshal(resp)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	withReleasesURL(t, server.URL)

	done := make(chan *UpdateInfo, 1)
	go func() {
		done <- CheckForUpdate("2.1.0")
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("fetch never started")
	}

	cached, err := readCache()
	require.NoError(t, err, "backoff cache must be on disk before the GitHub call returns")
	assert.Equal(t, "2.1.0", cached.LatestVersion)
	remaining := cacheTTL - time.Since(cached.CheckedAt)
	assert.InDelta(t, errorCacheTTL.Seconds(), remaining.Seconds(), 5)

	close(release)
	info := <-done
	require.NotNil(t, info)
	assert.Equal(t, "9.0.0", info.LatestVersion)
}

func TestCheckForUpdate_DoesNotRefetchDuringErrorBackoff(t *testing.T) {
	withCacheDir(t)

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	withReleasesURL(t, server.URL)

	assert.Nil(t, CheckForUpdate("2.1.0"))
	assert.Nil(t, CheckForUpdate("2.1.0"))
	assert.Equal(t, 1, hits, "second call should use the backoff cache instead of hitting GitHub")
}

func TestCheckForUpdate_PreservesKnownLatestOnFetchError(t *testing.T) {
	withCacheDir(t)

	writeCache(&cacheEntry{
		LatestVersion: "5.0.0",
		CheckedAt:     time.Now().Add(-cacheTTL - time.Minute),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	withReleasesURL(t, server.URL)

	info := CheckForUpdate("2.1.0")
	require.NotNil(t, info)
	assert.Equal(t, "5.0.0", info.LatestVersion)

	cached, err := readCache()
	require.NoError(t, err)
	assert.Equal(t, "5.0.0", cached.LatestVersion)
}

func TestCheckForUpdate_FetchesAndCachesNewerVersion(t *testing.T) {
	withCacheDir(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := githubRelease{TagName: "v5.0.0"}
		data, _ := json.Marshal(resp)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	withReleasesURL(t, server.URL)

	info := CheckForUpdate("2.1.0")
	require.NotNil(t, info)
	assert.Equal(t, "5.0.0", info.LatestVersion)

	cached, err := readCache()
	require.NoError(t, err)
	assert.Equal(t, "5.0.0", cached.LatestVersion)
	assert.Less(t, time.Since(cached.CheckedAt), time.Second)
}

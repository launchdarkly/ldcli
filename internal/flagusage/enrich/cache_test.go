package enrich

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
)

func TestCacheSetAndGet(t *testing.T) {
	origTTL := cacheTTL
	cacheTTL = 1 * time.Hour
	defer func() { cacheTTL = origTTL }()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	type testData struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	key := cacheKey("GET", "/api/v2/flags/default/my-flag", nil, nil)

	original := testData{Name: "my-flag", Value: 42}
	cacheSet(key, original)

	var result testData
	if !cacheGet(key, &result) {
		t.Fatal("expected cache hit")
	}
	if result.Name != "my-flag" || result.Value != 42 {
		t.Errorf("expected {my-flag, 42}, got {%s, %d}", result.Name, result.Value)
	}
}

func TestCacheMiss(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	var result struct{}
	if cacheGet("nonexistent.json", &result) {
		t.Error("expected cache miss for nonexistent key")
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	origTTL := cacheTTL
	cacheTTL = 1 * time.Millisecond
	defer func() { cacheTTL = origTTL }()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	key := cacheKey("GET", "/api/v2/test", nil, nil)
	cacheSet(key, map[string]string{"foo": "bar"})

	time.Sleep(5 * time.Millisecond)

	var result map[string]string
	if cacheGet(key, &result) {
		t.Error("expected cache miss after TTL expiry")
	}

	// Verify file was cleaned up
	cacheFile := filepath.Join(dir, ".cache", "ldcli", "flagusage", key)
	if _, err := os.Stat(cacheFile); !os.IsNotExist(err) {
		t.Error("expected expired cache file to be removed")
	}
}

func TestCacheKeyDiffersByHeaders(t *testing.T) {
	k1 := cacheKey("GET", "/api/v2/test", nil, nil)
	k2 := cacheKey("GET", "/api/v2/test", map[string]string{"LD-API-Version": "beta"}, nil)
	if k1 == k2 {
		t.Error("expected different cache keys for different headers")
	}
}

func TestCacheKeyDiffersByPath(t *testing.T) {
	k1 := cacheKey("GET", "/api/v2/flags/default/flag-a", nil, nil)
	k2 := cacheKey("GET", "/api/v2/flags/default/flag-b", nil, nil)
	if k1 == k2 {
		t.Error("expected different cache keys for different paths")
	}
}

func TestCacheKeyDiffersByMethodAndBody(t *testing.T) {
	get := cacheKey("GET", "/x", nil, nil)
	post := cacheKey("POST", "/x", nil, nil)
	if get == post {
		t.Error("expected different cache keys for different methods")
	}
	b1 := cacheKey("POST", "/x", nil, []byte(`{"flagKeys":["a"]}`))
	b2 := cacheKey("POST", "/x", nil, []byte(`{"flagKeys":["b"]}`))
	if b1 == b2 {
		t.Error("expected different cache keys for different request bodies")
	}
}

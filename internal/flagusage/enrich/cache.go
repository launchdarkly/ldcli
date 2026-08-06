package enrich

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/adrg/xdg"
)

var cacheTTL = 1 * time.Hour

func cacheDir() string {
	return filepath.Join(xdg.CacheHome, "ldcli", "flagusage")
}

func cacheKey(method, path string, headers map[string]string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte(path))
	// Hash headers in a stable order — map iteration order is random, which
	// would otherwise produce a different key for the same request.
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k + "=" + headers[k]))
	}
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))[:16] + ".json"
}

func cacheGet(key string, result any) bool {
	dir := cacheDir()
	if dir == "" {
		return false
	}
	path := filepath.Join(dir, key)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if time.Since(info.ModTime()) > cacheTTL {
		os.Remove(path)
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, result) == nil
}

func cacheSet(key string, value any) {
	dir := cacheDir()
	if dir == "" {
		return
	}
	os.MkdirAll(dir, 0o755)
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(dir, key), data, 0o644)
}

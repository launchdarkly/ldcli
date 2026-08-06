package enrich

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnrichFlag_NotFound_IsOrphanedReference(t *testing.T) {
	// HOME -> temp so the on-disk cache doesn't interfere.
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"not_found","message":"Invalid resource identifier"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient("api-test", srv.URL)
	detail, err := enrichFlag(client, "renamed-or-deleted-flag", Options{ProjectKey: "default"}, 3)
	if err != nil {
		t.Fatalf("a 404 on a referenced flag should not error, got %v", err)
	}
	if detail.StaleState != "orphanedReference" {
		t.Errorf("expected orphanedReference, got %q", detail.StaleState)
	}
	if detail.CallSites != 3 || detail.CodeRefs != 3 {
		t.Errorf("expected call sites preserved (3), got callSites=%d codeRefs=%d", detail.CallSites, detail.CodeRefs)
	}
}

func TestClient_404_WrapsErrNotFound(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient("api-test", srv.URL)
	var out map[string]any
	err := client.get("/api/v2/flags/default/nope", &out)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

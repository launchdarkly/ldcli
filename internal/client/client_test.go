package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/ldcli/internal/client"
)

func TestNewSendsAPIVersion(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Values("LD-API-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"_links": {}, "items": []}`))
	}))
	defer server.Close()

	_, _, err := client.New("token", server.URL, "test-version").ProjectsApi.GetProjects(context.Background()).Execute()

	require.NoError(t, err)
	assert.Equal(t, []string{"20240415"}, got)
}

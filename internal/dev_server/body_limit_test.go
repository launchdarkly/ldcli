package dev_server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimitRequestBody(t *testing.T) {
	handler := limitRequestBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name   string
		method string
		path   string
		size   int
		want   int
	}{
		{"at default limit", http.MethodPost, "/bulk", maxRequestBodyBytes, http.StatusOK},
		{"over default limit", http.MethodPost, "/bulk", maxRequestBodyBytes + 1, http.StatusRequestEntityTooLarge},
		{"backup restore above default limit", http.MethodPost, backupPath, maxRequestBodyBytes + 1, http.StatusOK},
		{"backup restore over backup limit", http.MethodPost, backupPath, maxBackupBodyBytes + 1, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(strings.Repeat("a", tt.size)))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, tt.want, rec.Code)
		})
	}
}

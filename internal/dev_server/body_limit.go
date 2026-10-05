package dev_server

import (
	"mime"
	"net/http"
)

const (
	maxRequestBodyBytes = 10 << 20
	maxBackupBodyBytes  = 256 << 20
	backupPath          = "/dev/backup"
	backupContentType   = "application/vnd.sqlite3"
)

func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxRequestBodyBytes)
		if r.Method == http.MethodPost && r.URL.Path == backupPath {
			// Requiring the documented content type forces a CORS preflight, so web pages can't
			// send restore requests.
			if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != backupContentType {
				http.Error(w, "Content-Type must be "+backupContentType, http.StatusUnsupportedMediaType)
				return
			}
			limit = maxBackupBodyBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

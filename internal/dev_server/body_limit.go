package dev_server

import "net/http"

const (
	maxRequestBodyBytes = 10 << 20
	maxBackupBodyBytes  = 256 << 20
	backupPath          = "/dev/backup"
)

func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxRequestBodyBytes)
		if r.Method == http.MethodPost && r.URL.Path == backupPath {
			limit = maxBackupBodyBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

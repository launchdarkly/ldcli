package ui

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
)

//go:embed dist/index.html
var content embed.FS

var AssetHandler = func() http.Handler {
	dist, err := fs.Sub(content, "dist")
	if err != nil {
		log.Fatalf("unable to open dist: %+v", err)
	}
	return http.FileServerFS(dist)
}()

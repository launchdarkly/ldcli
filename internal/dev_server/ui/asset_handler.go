package ui

import (
	"embed"
	"net/http"
)

//go:embed dist/index.html
var content embed.FS

var AssetHandler = http.FileServerFS(content)

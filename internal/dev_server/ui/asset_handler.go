package ui

import (
	"embed"
	"net/http"
)

//go:embed index.html
var content embed.FS

var AssetHandler = http.FileServerFS(content)

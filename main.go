package main

import (
	"net/http"
	"time"

	"ldcli/cmd"
	"ldcli/internal/analytics"
)

// main.version is set at build time via ldflags by go releaser https://goreleaser.com/cookbooks/using-main.version/
var (
	version = "dev"
)

func main() {
	httpClient := &http.Client{
		Timeout: time.Second * 3,
	}
	analyticsClient := &analytics.Client{HTTPClient: httpClient}
	cmd.Execute(analyticsClient, version)
	analyticsClient.Wait()
}

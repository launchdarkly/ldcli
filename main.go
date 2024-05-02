package main

import (
	"net/http"
	"time"

	"ldcli/cmd"
	"ldcli/internal/analytics"

	"github.com/google/uuid"
)

// main.version is set at build time via ldflags by go releaser https://goreleaser.com/cookbooks/using-main.version/
var (
	version = "dev"
)

func main() {
	httpClient := &http.Client{
		Timeout: time.Second * 3,
	}
	analyticsClient := &analytics.Client{
		HTTPClient: httpClient,
		ID:         uuid.New().String(),
		Version:    version,
	}
	cmd.Execute(analyticsClient, version)
	analyticsClient.Wait()
}

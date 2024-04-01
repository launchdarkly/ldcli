package main

import (
	segmentio "github.com/segmentio/analytics-go/v3"

	"ldcli/cmd"
	"ldcli/internal/analytics"
)

// main.version is set at build time via ldflags by go releaser https://goreleaser.com/cookbooks/using-main.version/
var (
	version = "dev"
)

func main() {
	client := analytics.NewSegmentioClient(
		segmentio.New("TODO"),
	)
	defer client.Close()

	cmd.Execute(client, version)
}

package main

import (
	"fmt"

	segmentio "github.com/segmentio/analytics-go/v3"

	"ldcli/cmd"
	"ldcli/internal/analytics"
)

// main.version is set at build time via ldflags by go releaser https://goreleaser.com/cookbooks/using-main.version/
var (
	version         = "dev"
	segmentWriteKey = ""
)

func main() {
	fmt.Println(">>> segmentWriteKey", segmentWriteKey)
	client := analytics.NewSegmentioClient(
		segmentio.New(segmentWriteKey),
	)
	defer client.Close()

	cmd.Execute(client, version)
}

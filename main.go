package main

import (
	"github.com/launchdarkly/ldcli/cmd"
)

// main.version is set at build time via ldflags by go releaser https://goreleaser.com/cookbooks/using-main.version/
var (
	version = "dev"
)

func main() {
	cmd.Execute(version)
}

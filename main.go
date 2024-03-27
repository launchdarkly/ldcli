package main

import "ldcli/cmd"

var (
	version = "dev"
)

func main() {
	cmd.Execute(version)
}

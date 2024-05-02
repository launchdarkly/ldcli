//go:build gen_resources
// +build gen_resources

package main

import (
	"log"

	"ldcli/cmd/resources"
)

const pathSpecFile = "../ld-teams-openapi.json"

func main() {
	log.Println("Generating resources...")
	_, err := resources.GetTemplateData(pathSpecFile)
	if err != nil {
		panic(err)
	}
}

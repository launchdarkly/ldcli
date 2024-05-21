//go:build gen_resources
// +build gen_resources

package main

import (
	"bytes"
	"go/format"
	"io/ioutil"
	"log"
	"text/template"

	"github.com/launchdarkly/ldcli/cmd/resources"
)

const (
	pathSpecFile = "../ld-openapi.json"
	pathTemplate = "resources/resource_cmds.tmpl"
	templateName = "resource_cmds.tmpl"
	pathOutput   = "resources/resource_cmds.go"
)

func main() {
	log.Println("Generating resources...")
	templateData, err := resources.GetTemplateData(pathSpecFile)
	if err != nil {
		panic(err)
	}

	tmpl, err := template.New(templateName).ParseFiles(pathTemplate)
	if err != nil {
		panic(err)
	}

	var result bytes.Buffer
	err = tmpl.Execute(&result, templateData)
	if err != nil {
		panic(err)
	}

	// Format the output of the template execution
	formatted, err := format.Source(result.Bytes())
	if err != nil {
		panic(err)
	}

	// Write the formatted source code to disk
	log.Printf("writing %s\n", pathOutput)
	err = ioutil.WriteFile(pathOutput, formatted, 0644)
	if err != nil {
		panic(err)
	}
}

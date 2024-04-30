package main

import (
	"github.com/getkin/kin-openapi/openapi3"
	"log"
	"os"
)

type TemplateData struct {
	Resources []ResourceData
}

type ResourceData struct {
	Name       string
	Operations []OperationData
}

type OperationData struct {
	Short        string
	Long         string
	Use          string
	Params       []Param
	HTTPMethod   string
	RequiresBody bool
	Path         string
}

type Param struct {
	Name        string
	In          string
	Description string
	Type        string
	Required    bool
}

const pathSpecFile = "ld-teams-openapi.json"

func main() {
	getTemplateData()
}

func getTemplateData() (TemplateData, error) {
	rawFile, err := os.ReadFile(pathSpecFile)
	if err != nil {
		return TemplateData{}, err
	}

	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromData(rawFile)
	if err != nil {
		return TemplateData{}, nil
	}

	for k, v := range spec.Paths.Map() {
		log.Println(k, ":", v)
	}

	return TemplateData{}, nil
}

package resources

import (
	"fmt"
	"log"
	"os"

	"github.com/getkin/kin-openapi/openapi3"

	"ldcli/internal/errors"
)

type TemplateData struct {
	Resources map[string]*ResourceData
}

type ResourceData struct {
	Name        string
	Description string
	Operations  []*OperationData
}

type OperationData struct {
	Short        string
	Long         string
	Use          string
	Params       []*Param
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

func GetTemplateData() (TemplateData, error) {
	rawFile, err := os.ReadFile(pathSpecFile)
	if err != nil {
		return TemplateData{}, err
	}

	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromData(rawFile)
	if err != nil {
		return TemplateData{}, err
	}

	resources := make(map[string]*ResourceData)
	for _, r := range spec.Tags {
		resources[r.Name] = &ResourceData{
			Name:        r.Name,
			Description: r.Description,
			Operations:  make([]*OperationData, 0),
		}
	}

	for path, pathItem := range spec.Paths.Map() {
		for method, op := range pathItem.Operations() {
			// TODO: confirm each op only has one tag
			tag := op.Tags[0]
			resource := resources[tag]
			if resource == nil {
				return TemplateData{}, errors.NewError(fmt.Sprintf("Matching resource not found for operation's tag: %s", tag))
			}

			use := methodToCmdUse(method)
			schema := spec.Components.Schemas[tag]
			if schema == nil {
				log.Printf("No schema found for %s", tag)
			} else {
				for k, _ := range schema.Value.Properties {
					if k == "items" {
						use = "list"
						break
					}
				}
			}

			operation := OperationData{
				Short:        op.Summary,
				Long:         op.Description,
				Use:          use,
				Params:       make([]*Param, 0),
				HTTPMethod:   method,
				RequiresBody: method == "PUT" || method == "POST" || method == "PATCH",
				Path:         path,
			}

			for _, p := range op.Parameters {
				if p.Value != nil {
					// TODO: confirm if we only have one type per param b/c somehow this is a slice
					types := *p.Value.Schema.Value.Type
					param := Param{
						Name:        p.Value.Name,
						In:          p.Value.In,
						Description: p.Value.Description,
						Type:        types[0],
						Required:    p.Value.Required,
					}
					operation.Params = append(operation.Params, &param)
				}
			}

			resource.Operations = append(resource.Operations, &operation)
		}
	}

	return TemplateData{Resources: resources}, nil
}

func methodToCmdUse(method string) string {
	methodMap := map[string]string{
		"GET":    "get",
		"POST":   "create",
		"PUT":    "replace", // TODO: confirm this
		"DELETE": "delete",
		"PATCH":  "update",
	}

	return methodMap[method]
}

package resources

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

type TemplateData struct {
	Resources map[string]*ResourceData
}

type ResourceData struct {
	Name        string
	Description string
	Operations  map[string]*OperationData
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

func GetTemplateData(fileName string) (TemplateData, error) {
	rawFile, err := os.ReadFile(fileName)
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
			Operations:  make(map[string]*OperationData, 0),
		}
	}

	for path, pathItem := range spec.Paths.Map() {
		for method, op := range pathItem.Operations() {
			tag := op.Tags[0] // TODO: confirm each op only has one tag
			resource := resources[tag]
			if resource == nil {
				log.Printf("Matching resource not found for %s operation's tag: %s", op.OperationID, tag)
				continue
			}

			use := getCmdUse(method, op, spec)

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

			resource.Operations[op.OperationID] = &operation
		}
	}

	return TemplateData{Resources: resources}, nil
}

func getCmdUse(method string, op *openapi3.Operation, spec *openapi3.T) string {
	methodMap := map[string]string{
		"GET":    "get",
		"POST":   "create",
		"PUT":    "replace", // TODO: confirm this
		"DELETE": "delete",
		"PATCH":  "update",
	}

	use := methodMap[method]

	var schema *openapi3.SchemaRef
	for respType, respInfo := range op.Responses.Map() {
		respCode, _ := strconv.Atoi(respType)
		if respCode < 300 {
			for _, s := range respInfo.Value.Content {
				schemaName := strings.TrimPrefix(s.Schema.Ref, "#/components/schemas/")
				schema = spec.Components.Schemas[schemaName]
			}
		}
	}

	if schema == nil {
		// probably won't need to keep this logging in but leaving it for debugging purposes
		log.Printf("No response type defined for %s", op.OperationID)
	} else {
		for propName, _ := range schema.Value.Properties {
			if propName == "items" {
				use = "list"
				break
			}
		}
	}
	return use
}

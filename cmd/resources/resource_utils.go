package resources

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/iancoleman/strcase"
)

// we have certain tags that aren't a 1:1 match to their operation id names
var mapTagToSchemaName = map[string]string{
	"Access tokens":   "Tokens",
	"Account members": "Members",
	"Approvals":       "Approval requests",
	"Code references": "Code refs",
	"OAuth2 Clients":  "Oauth2 clients", // this is just so we don't kebab case to o-auth
	"User settings":   "User flag settings",
}

func getResourceNames(name string) (string, string) {
	if mappedName, ok := mapTagToSchemaName[name]; ok {
		name = mappedName
	}

	resourceKey := strcase.ToKebab(name)
	// the operationIds use "FeatureFlag" so we want to keep that whole, but the command should just be `flags`
	if name == "Feature flags" {
		resourceKey = "flags"
	}
	return name, resourceKey
}

func replaceMethodWithCmdUse(operationId string) string {
	r := strings.NewReplacer(
		"post", "create",
		"patch", "update",
		"put", "replace",
	)

	return r.Replace(operationId)
}

func removeResourceFromOperationId(resourceName, operationId string) string {
	// operations use both singular (Team) and plural (Teams) resource names, whereas resource names are (usually) plural
	var singularResourceName string
	if string(resourceName[len(resourceName)-1]) == "s" {
		singularResourceName = resourceName[:len(resourceName)-1]
	}

	r := strings.NewReplacer(
		// a lot of "list" operations say "GetFor{ResourceName}"
		fmt.Sprintf("For%s", singularResourceName), "",
		fmt.Sprintf("For%s", resourceName), "",
		fmt.Sprint("ByProject"), "",
		resourceName, "",
		singularResourceName, "",
	)

	return r.Replace(operationId)
}

func getCmdUse(resourceName, operationId string, isList bool) string {
	action := removeResourceFromOperationId(resourceName, operationId)
	action = strcase.ToKebab(action)
	action = replaceMethodWithCmdUse(action)

	if isList {
		re := regexp.MustCompile(`^get(.*)$`)
		action = re.ReplaceAllString(action, "list$1")
	}

	return action
}

func isListResponse(op *openapi3.Operation, spec *openapi3.T) bool {
	// get the success response type from the operation to retrieve its schema
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

	// if the schema is nil, there is no response body for the request
	if schema != nil {
		for propName := range schema.Value.Properties {
			if propName == "items" {
				return true
			}
		}
	}
	return false
}

var mapParamToFlagName = map[string]string{
	"feature-flag": "flag",
}

func stripFlagName(flagName string) string {
	r := strings.NewReplacer(
		"-key", "",
		"-id", "",
	)

	return r.Replace(flagName)
}

func getFlagName(paramName string) string {
	flagName := strcase.ToKebab(paramName)
	flagName = stripFlagName(flagName)
	if mappedName, ok := mapParamToFlagName[flagName]; ok {
		flagName = mappedName
	}
	return flagName
}

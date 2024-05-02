package resources

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "ldcli/cmd/analytics"
	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/analytics"
	"ldcli/internal/errors"
	"ldcli/internal/output"
	"ldcli/internal/resources"
)

type TemplateData struct {
	Resources map[string]ResourceData
}

type ResourceData struct {
	Name        string
	Description string
	Operations  map[string]OperationData
}

type OperationData struct {
	Short                 string
	Long                  string
	Use                   string
	Params                []Param
	HTTPMethod            string
	RequiresBody          bool
	Path                  string
	SupportsSemanticPatch bool
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

	resources := make(map[string]ResourceData)
	for _, r := range spec.Tags {
		resources[r.Name] = ResourceData{
			Name:        r.Name,
			Description: r.Description,
			Operations:  make(map[string]OperationData, 0),
		}
	}

	for path, pathItem := range spec.Paths.Map() {
		for method, op := range pathItem.Operations() {
			tag := op.Tags[0] // TODO: confirm each op only has one tag
			resource, ok := resources[tag]
			if !ok {
				log.Printf("Matching resource not found for %s operation's tag: %s", op.OperationID, tag)
				continue
			}

			use := getCmdUse(method, op, spec)

			operation := OperationData{
				Short:        op.Summary,
				Long:         op.Description,
				Use:          use,
				Params:       make([]Param, 0),
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
					operation.Params = append(operation.Params, param)
				}
			}

			resource.Operations[op.OperationID] = operation
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
		for propName := range schema.Value.Properties {
			if propName == "items" {
				use = "list"
				break
			}
		}
	}
	return use
}

func NewResourceCmd(parentCmd *cobra.Command, analyticsTracker analytics.Tracker, resourceName, shortDescription, longDescription string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   resourceName,
		Short: shortDescription,
		Long:  longDescription,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			analyticsTracker.SendCommandRunEvent(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
				cmdAnalytics.CmdRunEventProperties(cmd, resourceName),
			)
		},
	}

	parentCmd.AddCommand(cmd)

	return cmd
}

type OperationCmd struct {
	OperationData
	client resources.Client
	cmd    *cobra.Command
}

func (op *OperationCmd) initFlags() error {
	if op.RequiresBody {
		op.cmd.Flags().StringP(cliflags.DataFlag, "d", "", "Input data in JSON")
		err := op.cmd.MarkFlagRequired(cliflags.DataFlag)
		if err != nil {
			return err
		}
		err = viper.BindPFlag(cliflags.DataFlag, op.cmd.Flags().Lookup(cliflags.DataFlag))
		if err != nil {
			return err
		}
	}

	if op.SupportsSemanticPatch {
		op.cmd.Flags().Bool("semantic-patch", false, "Perform a semantic patch request")
		err := viper.BindPFlag("semantic-patch", op.cmd.Flags().Lookup("semantic-patch"))
		if err != nil {
			return err
		}
	}

	for _, p := range op.Params {
		shorthand := fmt.Sprintf(p.Name[0:1]) // todo: how do we handle potential dupes
		// TODO: consider handling these all as strings
		switch p.Type {
		case "string":
			op.cmd.Flags().StringP(p.Name, shorthand, "", p.Description)
		case "int":
			op.cmd.Flags().IntP(p.Name, shorthand, 0, p.Description)
		case "boolean":
			op.cmd.Flags().BoolP(p.Name, shorthand, false, p.Description)
		}

		if p.In == "path" {
			err := op.cmd.MarkFlagRequired(p.Name)
			if err != nil {
				return err
			}
		}

		err := viper.BindPFlag(p.Name, op.cmd.Flags().Lookup(p.Name))
		if err != nil {
			return err
		}
	}
	return nil
}

func buildURLWithParams(baseURI, path string, urlParams []string) string {
	s := make([]interface{}, len(urlParams))
	for i, v := range urlParams {
		s[i] = v
	}

	re := regexp.MustCompile(`{\w+}`)
	format := re.ReplaceAllString(path, "%s")

	return baseURI + fmt.Sprintf(format, s...)
}

func (op *OperationCmd) makeRequest(cmd *cobra.Command, args []string) error {
	var data interface{}
	if op.RequiresBody {
		// TODO: why does viper.GetString(cliflags.DataFlag) not work?
		err := json.Unmarshal([]byte(cmd.Flags().Lookup(cliflags.DataFlag).Value.String()), &data)
		if err != nil {
			return err
		}
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	query := url.Values{}
	var urlParms []string
	for _, p := range op.Params {
		val := viper.GetString(p.Name)
		if val != "" {
			switch p.In {
			case "path":
				urlParms = append(urlParms, val)
			case "query":
				query.Add(p.Name, val)
			}
		}
	}

	path := buildURLWithParams(viper.GetString(cliflags.BaseURIFlag), op.Path, urlParms)

	contentType := "application/json"
	if viper.GetBool("semantic-patch") {
		contentType += "; domain-model=launchdarkly.semanticpatch"
	}

	res, err := op.client.MakeRequest(
		viper.GetString(cliflags.AccessTokenFlag),
		strings.ToUpper(op.HTTPMethod),
		path,
		contentType,
		query,
		jsonData,
	)
	if err != nil {
		return errors.NewError(output.CmdOutputError(viper.GetString(cliflags.OutputFlag), err))
	}

	output, err := output.CmdOutput("get", viper.GetString(cliflags.OutputFlag), res)
	if err != nil {
		return errors.NewError(err.Error())
	}

	fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

	return nil
}

func NewOperationCmd(parentCmd *cobra.Command, client resources.Client, op OperationData) *cobra.Command {
	opCmd := OperationCmd{
		OperationData: op,
		client:        client,
	}

	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  op.Long,
		RunE:  opCmd.makeRequest,
		Short: op.Short,
		Use:   op.Use,
	}

	opCmd.cmd = cmd
	_ = opCmd.initFlags()

	parentCmd.AddCommand(cmd)

	return cmd
}

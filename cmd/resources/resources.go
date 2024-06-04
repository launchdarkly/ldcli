package resources

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/iancoleman/strcase"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/interactive_mode"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

func getResourcesHelpTemplate() string {
	// This template uses `.Parent` to access subcommands on the root command.
	return `Available commands:{{range $index, $cmd := .Parent.Commands}}{{if (or (eq (index $.Parent.Annotations $cmd.Name) "resource"))}}
  {{rpad $cmd.Name $cmd.NamePadding }} {{$cmd.Short}}{{end}}{{end}}

Use "ldcli [command] --help" for more information about a command.
`
}

func NewResourcesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "List resource commands",
	}
	cmd.SetHelpTemplate(getResourcesHelpTemplate())

	return cmd
}

type TemplateData struct {
	Resources map[string]ResourceData
}

type ResourceData struct {
	GoName      string
	DisplayName string
	Description string
	Operations  map[string]OperationData
}

type OperationData struct {
	Short                 string
	Long                  string
	Use                   string
	Params                []Param
	HTTPMethod            string
	HasBody               bool
	IsBeta                bool
	RequiresBody          bool
	Path                  string
	SupportsSemanticPatch bool
	FormInputs            []interactive_mode.Input
}

type Param struct {
	Name        string
	In          string
	Description string
	Type        string
	Required    bool
}

func jsonString(s string) string {
	bs, _ := json.Marshal(s)
	return string(bs)
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

	resources := NewResources(spec.Tags)
	for path, pathItem := range spec.Paths.Map() {
		for method, op := range pathItem.Operations() {
			tag := op.Tags[0] // each op only has one tag
			if shouldFilter(tag) {
				continue
			}
			_, resourceKey := getResourceNames(tag)
			resource, ok := resources[resourceKey]
			if !ok {
				log.Printf("Matching resource not found for %s operation's tag: %s", op.OperationID, resourceKey)
				continue
			}

			isList := isListResponse(op, spec)
			use := getCmdUse(resource.GoName, op.OperationID, isList)

			var supportsSemanticPatch bool
			if strings.Contains(op.Description, "semantic patch") {
				supportsSemanticPatch = true
			}

			var hasBody, requiresBody bool
			var formInputs []interactive_mode.Input
			if op.RequestBody != nil {
				hasBody = true
				requiresBody = op.RequestBody.Value.Required
				formInputs = getFormInputs(op, spec)
			}

			isBeta := strings.Contains(tag, "(beta)")

			operation := OperationData{
				Short:                 jsonString(op.Summary),
				Long:                  jsonString(op.Description),
				Use:                   use,
				Params:                make([]Param, 0),
				HTTPMethod:            method,
				HasBody:               hasBody,
				IsBeta:                isBeta,
				RequiresBody:          requiresBody,
				Path:                  path,
				SupportsSemanticPatch: supportsSemanticPatch,
				FormInputs:            formInputs,
			}

			for _, p := range op.Parameters {
				if p.Value != nil {
					if strings.Contains(p.Value.Description, "Deprecated") {
						continue
					}
					// TODO: confirm if we only have one type per param b/c somehow this is a slice
					types := *p.Value.Schema.Value.Type

					// cobra will try to take backquoted values as the type, so we remove them
					description := replaceBackticks(p.Value.Description)

					param := Param{
						Name:        strcase.ToKebab(p.Value.Name),
						In:          p.Value.In,
						Description: jsonString(description),
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

func NewResourceData(tag openapi3.Tag) ResourceData {
	resourceName, _ := getResourceNames(tag.Name)

	return ResourceData{
		GoName:      strcase.ToCamel(resourceName),
		DisplayName: strings.ToLower(resourceName),
		Description: jsonString(tag.Description),
		Operations:  make(map[string]OperationData, 0),
	}
}

func NewResources(tags openapi3.Tags) map[string]ResourceData {
	resources := make(map[string]ResourceData)
	for _, t := range tags {
		if shouldFilter(t.Name) {
			continue
		}

		resourceName, resourceKey := getResourceNames(t.Name)
		resources[resourceKey] = ResourceData{
			GoName:      strcase.ToCamel(resourceName),
			DisplayName: strings.ToLower(resourceName),
			Description: jsonString(t.Description),
			Operations:  make(map[string]OperationData, 0),
		}
	}

	return resources
}

func shouldFilter(name string) bool {
	return strings.Contains(name, "User") ||
		strings.ToLower(name) == "other" ||
		strings.ToLower(name) == "oauth2 clients"
}

func NewResourceCmd(
	parentCmd *cobra.Command,
	analyticsTrackerFn analytics.TrackerFn,
	markdownRenderer *glamour.TermRenderer,
	resourceName, longDescription string,
) *cobra.Command {
	longAsMarkdown, _ := markdownRenderer.Render(longDescription)
	cmd := &cobra.Command{
		Use:  resourceName,
		Long: longAsMarkdown,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			tracker := analyticsTrackerFn(
				viper.GetString(cliflags.AccessTokenFlag),
				viper.GetString(cliflags.BaseURIFlag),
				viper.GetBool(cliflags.AnalyticsOptOut),
			)
			tracker.SendCommandRunEvent(cmdAnalytics.CmdRunEventProperties(cmd, resourceName, nil))
		},
	}

	cmd.SetUsageTemplate(SubcommandUsageTemplate())
	parentCmd.AddCommand(cmd)
	parentCmd.Annotations[resourceName] = "resource"

	return cmd
}

type OperationCmd struct {
	OperationData
	client resources.Client
	cmd    *cobra.Command
}

func (op *OperationCmd) initFlags() error {
	if op.HasBody {
		op.cmd.Flags().StringP(cliflags.DataFlag, "d", "", "Input data in JSON")
		op.cmd.Flags().BoolP(cliflags.InteractiveFlag, "i", false, "Optionally enter data using a form")

		if op.RequiresBody {
			if true { // eventually we'll want to check if the request body is form-compatabile, probably
				op.cmd.MarkFlagsOneRequired(cliflags.DataFlag, cliflags.InteractiveFlag)
			} else {
				err := op.cmd.MarkFlagRequired(cliflags.DataFlag)
				if err != nil {
					return err
				}
			}

			// still going to mark --data as required for the help template to display
			err := op.cmd.Flags().SetAnnotation(cliflags.DataFlag, "required", []string{"true"})
			if err != nil {
				return err
			}
		}
		err := viper.BindPFlag(cliflags.DataFlag, op.cmd.Flags().Lookup(cliflags.DataFlag))
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
		flagName := getFlagName(p.Name)

		op.cmd.Flags().String(flagName, "", p.Description)

		if p.In == "path" || p.Required {
			err := op.cmd.MarkFlagRequired(flagName)
			if err != nil {
				return err
			}
			_ = op.cmd.Flags().SetAnnotation(flagName, "required", []string{"true"})
		}

		err := viper.BindPFlag(flagName, op.cmd.Flags().Lookup(flagName))
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
		if viper.GetBool("interactive") {
			f, err := tea.LogToFile("debug.log", "")
			if err != nil {
				fmt.Println("could not open file for debuggin", err)
				os.Exit(1)
			}
			defer f.Close()
			_, err = tea.NewProgram(
				interactive_mode.NewInteractiveInputModel(op.cmd.Parent().Use, op.cmd.Use, op.FormInputs),
				tea.WithAltScreen(),
			).Run()
			if err != nil {
				log.Fatal(err)
			}
			return nil
		} else {
			err := json.Unmarshal([]byte(viper.GetString(cliflags.DataFlag)), &data)
			if err != nil {
				return err
			}
		}

	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	query := url.Values{}
	var urlParms []string
	for _, p := range op.Params {
		flagName := getFlagName(p.Name)
		val := viper.GetString(flagName)
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
		op.IsBeta,
	)
	if err != nil {
		return errors.NewError(output.CmdOutputError(viper.GetString(cliflags.OutputFlag), err))
	}

	if string(res) == "" {
		// assuming the key to be deleted/replaced is last in the list of params,
		// e.g. contexts delete-instances or code-refs replace-branch
		res = []byte(fmt.Sprintf(`{"key": %q}`, urlParms[len(urlParms)-1]))
	}

	output, err := output.CmdOutput(cmd.Use, viper.GetString(cliflags.OutputFlag), res)
	if err != nil {
		return errors.NewError(err.Error())
	}

	fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

	return nil
}

func NewOperationCmd(
	parentCmd *cobra.Command,
	client resources.Client,
	markdownRenderer *glamour.TermRenderer,
	op OperationData,
) *cobra.Command {
	opCmd := OperationCmd{
		OperationData: op,
		client:        client,
	}
	longAsMarkdown, _ := markdownRenderer.Render(op.Long)

	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  longAsMarkdown,
		RunE:  opCmd.makeRequest,
		Short: op.Short,
		Use:   op.Use,
	}

	cmd.SetUsageTemplate(SubcommandUsageTemplate())

	opCmd.cmd = cmd
	_ = opCmd.initFlags()

	parentCmd.AddCommand(cmd)

	return cmd
}

func SubcommandUsageTemplate() string {
	return `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}{{if HasRequiredFlags .}}

Required flags:
{{WrappedRequiredFlagUsages . | trimTrailingWhitespaces}}{{end}}{{if HasOptionalFlags .}}

Optional flags:
{{WrappedOptionalFlagUsages . | trimTrailingWhitespaces}}{{end}}

Global flags:
{{rpad "  -h, --help" 29}} Get help about any command
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
}

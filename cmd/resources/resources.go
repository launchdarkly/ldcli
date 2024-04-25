package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io"
	"ldcli/internal/errors"
	"ldcli/internal/output"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	cmdAnalytics "ldcli/cmd/analytics"
	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
	"ldcli/internal/analytics"
)

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

type OperationData struct {
	Short                 string
	Long                  string
	Use                   string
	Params                []Param
	HTTPMethod            string
	RequiresBody          bool
	Path                  string
	SupportsSemanticPatch bool // TBD on how to actually determine from openapi spec
	PlaintextOutputFn     output.PlaintextOutputFn
}

type Param struct {
	Name        string
	In          string
	Description string
	Type        string
	Required    bool
}

type OperationCmd struct {
	OperationData
	client *http.Client
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

func formatURL(baseURI, path string, urlParams []string) string {
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

	query := url.Values{}
	var urlParms []string
	for _, p := range op.Params {
		val := viper.GetString(p.Name)
		if val != "" {
			switch p.In {
			case "path":
				urlParms = append(urlParms, fmt.Sprintf("%v", val))
			case "query":
				query.Add(p.Name, fmt.Sprintf("%v", val))
			}
		}
	}

	contentType := "application/json"
	if op.SupportsSemanticPatch && viper.GetBool("semantic-patch") {
		contentType += "; domain-model=launchdarkly.semanticpatch"
	}

	path := formatURL(viper.GetString(cliflags.BaseURIFlag), op.Path, urlParms)

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, _ := http.NewRequest(strings.ToUpper(op.HTTPMethod), path, bytes.NewReader(jsonData))
	req.Header.Add("Authorization", viper.GetString(cliflags.AccessTokenFlag))
	req.Header.Add("Content-type", contentType)
	req.URL.RawQuery = query.Encode()

	res, err := op.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode >= 400 {
		err = errors.NewAPIError(body, nil, nil)
		output, err := output.CmdOutputSingular(
			viper.GetString(cliflags.OutputFlag),
			body,
			output.ErrorPlaintextOutputFn,
		)
		if err != nil {
			return errors.NewError(err.Error())
		}

		return errors.NewError(output)
	}

	output, err := output.CmdOutputSingular(
		viper.GetString(cliflags.OutputFlag),
		body,
		op.PlaintextOutputFn,
	)
	if err != nil {
		return errors.NewError(err.Error())
	}

	fmt.Fprintf(cmd.OutOrStdout(), output+"\n")

	return nil
}

func NewOperationCmd(parentCmd *cobra.Command, client *http.Client, op OperationData) *cobra.Command {
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
		//TODO: add tracking here
	}

	opCmd.cmd = cmd
	_ = opCmd.initFlags()

	parentCmd.AddCommand(cmd)

	return cmd
}

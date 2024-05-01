package resources

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

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

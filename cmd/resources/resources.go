package resources

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	"ldcli/cmd/validators"
)

func NewResourceCmd(parentCmd *cobra.Command, resourceName, shortDescription, longDescription string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   resourceName,
		Short: shortDescription,
		Long:  longDescription,
	}

	parentCmd.AddCommand(cmd)

	return cmd
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

	for _, p := range op.Params {
		shorthand := fmt.Sprintf(p.Name[0:1]) // todo: how do we handle potential dupes
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

func (op *OperationCmd) makeRequest(cmd *cobra.Command, args []string) error {
	paramVals := map[string]interface{}{}

	if op.RequiresBody {
		var data interface{}
		// TODO: why does viper.GetString(cliflags.DataFlag) not work?
		err := json.Unmarshal([]byte(cmd.Flags().Lookup(cliflags.DataFlag).Value.String()), &data)
		if err != nil {
			return err
		}
		paramVals[cliflags.DataFlag] = data
	}

	for _, p := range op.Params {
		var val interface{}
		switch p.Type {
		case "string":
			val = viper.GetString(p.Name)
		case "boolean":
			val = viper.GetBool(p.Name)
		case "int":
			val = viper.GetInt(p.Name)
		}

		if val != nil {
			paramVals[p.Name] = val
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "would be making a %s request to %s here, with args: %s\n", op.HTTPMethod, op.Path, paramVals)

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

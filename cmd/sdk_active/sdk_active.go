package sdk_active

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
)

type sdkActiveResponse struct {
	Active bool `json:"active"`
}

func NewSdkActiveCmd(client resources.Client) *cobra.Command {
	cmd := &cobra.Command{
		Args:  validators.Validate(),
		Long:  "Get SDK active status for an environment. Returns information about whether any SDKs have initialized in the given environment within the past seven days.",
		RunE:  runGetSdkActive(client),
		Short: "Get SDK active status for an environment",
		Use:   "get-sdk-active",
	}

	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())
	initFlags(cmd)

	return cmd
}

const (
	sdkNameFlag        = "sdk-name"
	sdkWrapperNameFlag = "sdk-wrapper-name"
)

func runGetSdkActive(client resources.Client) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		path, _ := url.JoinPath(
			viper.GetString(cliflags.BaseURIFlag),
			"api/v2/projects",
			viper.GetString(cliflags.ProjectFlag),
			"environments",
			viper.GetString(cliflags.EnvironmentFlag),
			"sdk-active",
		)

		query := url.Values{}
		if v := viper.GetString(sdkNameFlag); v != "" {
			query.Set("sdk_name", v)
		}
		if v := viper.GetString(sdkWrapperNameFlag); v != "" {
			query.Set("sdk_wrapper_name", v)
		}

		res, err := client.MakeRequest(
			viper.GetString(cliflags.AccessTokenFlag),
			"GET",
			path,
			"application/json",
			query,
			nil,
			false,
		)
		if err != nil {
			return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
		}

		outputKind := cliflags.GetOutputKind(cmd)
		if outputKind == "json" {
			fmt.Fprint(cmd.OutOrStdout(), string(res)+"\n")
			return nil
		}

		var resp sdkActiveResponse
		if err := json.Unmarshal(res, &resp); err != nil {
			return errors.NewError(err.Error())
		}

		fmt.Fprintf(cmd.OutOrStdout(), "SDK active: %t\n", resp.Active)

		return nil
	}
}

func initFlags(cmd *cobra.Command) {
	cmd.Flags().String(cliflags.ProjectFlag, "", "The project key")
	_ = cmd.MarkFlagRequired(cliflags.ProjectFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.ProjectFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.ProjectFlag, cmd.Flags().Lookup(cliflags.ProjectFlag))

	cmd.Flags().String(cliflags.EnvironmentFlag, "", "The environment key")
	_ = cmd.MarkFlagRequired(cliflags.EnvironmentFlag)
	_ = cmd.Flags().SetAnnotation(cliflags.EnvironmentFlag, "required", []string{"true"})
	_ = viper.BindPFlag(cliflags.EnvironmentFlag, cmd.Flags().Lookup(cliflags.EnvironmentFlag))

	cmd.Flags().String(sdkNameFlag, "", "Filter by SDK name (e.g. go-server-sdk, node-server-sdk)")
	_ = viper.BindPFlag(sdkNameFlag, cmd.Flags().Lookup(sdkNameFlag))

	cmd.Flags().String(sdkWrapperNameFlag, "", "Filter by SDK wrapper name")
	_ = viper.BindPFlag(sdkWrapperNameFlag, cmd.Flags().Lookup(sdkWrapperNameFlag))
}

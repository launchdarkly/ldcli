package setup

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/setup"
)

func getFlag(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

const (
	fileFlag     = "file"
	sdkKeyFlag   = "sdk-key"
	clientIDFlag = "client-side-id"
	mobileFlag   = "mobile-key"
	flagKeyFlag  = "flag-key"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "init",
		Short:  "Inject LaunchDarkly SDK initialization code into a file",
		Hidden: true,
		RunE:   runInit(),
	}

	cmd.Flags().String(sdkIDFlag, "", "SDK identifier (e.g. node-server, react-client-sdk)")
	_ = cmd.MarkFlagRequired(sdkIDFlag)

	cmd.Flags().String(fileFlag, "", "Target file to inject initialization code into")
	_ = cmd.MarkFlagRequired(fileFlag)

	cmd.Flags().String(sdkKeyFlag, "", "Server-side SDK key")
	cmd.Flags().String(clientIDFlag, "", "Client-side environment ID")
	cmd.Flags().String(mobileFlag, "", "Mobile SDK key")
	cmd.Flags().String(flagKeyFlag, "", "Feature flag key to use in the initialization example")

	return cmd
}

func runInit() func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		sdkID, _ := cmd.Flags().GetString(sdkIDFlag)
		filePath, _ := cmd.Flags().GetString(fileFlag)
		cfg := setup.InitConfig{
			SDKKey:       getFlag(cmd, sdkKeyFlag),
			ClientSideID: getFlag(cmd, clientIDFlag),
			MobileKey:    getFlag(cmd, mobileFlag),
			FlagKey:      getFlag(cmd, flagKeyFlag),
		}

		initializer := setup.Initializer{}
		result, err := initializer.InjectIntoFile(sdkID, filePath, cfg)
		if err != nil {
			return err
		}

		outputKind := cliflags.GetOutputKind(cmd)
		if outputKind == "json" {
			data, _ := json.Marshal(result)
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Injected %s initialization into %s\n", result.SDKID, result.FilePath)
		return nil
	}
}

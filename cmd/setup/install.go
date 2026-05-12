package setup

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/setup"
)

const sdkIDFlag = "sdk-id"

func newInstallCmd(installer setup.Installer) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "install",
		Short:  "Install the LaunchDarkly SDK package for the detected project",
		Hidden: true,
		RunE:   runInstall(installer),
	}

	cmd.Flags().String(pathFlag, "", "Path to the project directory (defaults to current directory)")
	cmd.Flags().String(sdkIDFlag, "", "SDK identifier to install (e.g. node-server, react-client-sdk)")
	_ = cmd.MarkFlagRequired(sdkIDFlag)
	cmd.Flags().String("package-manager", "", "Package manager to use (e.g. npm, pip, go)")

	return cmd
}

func runInstall(installer setup.Installer) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString(pathFlag)
		if dir == "" {
			var err error
			dir, err = os.Getwd()
			if err != nil {
				return err
			}
		}

		sdkID, _ := cmd.Flags().GetString(sdkIDFlag)
		pkgMgr, _ := cmd.Flags().GetString("package-manager")
		detection := &setup.DetectResult{
			SDKID:          sdkID,
			PackageManager: pkgMgr,
		}

		result, err := installer.Install(dir, detection)
		if err != nil {
			return err
		}

		outputKind := cliflags.GetOutputKind(cmd)
		if outputKind == "json" {
			data, _ := json.Marshal(result)
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "SDK: %s\n", result.SDKID)
		if result.Version != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Package: %s@%s\n", result.Package, result.Version)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Package: %s\n", result.Package)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Command: %s\n", result.Command)
		fmt.Fprintf(cmd.OutOrStdout(), "Success: %t\n", result.Success)

		return nil
	}
}

package setup

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/setup"
)

const pathFlag = "path"

func newDetectCmd(detector setup.Detector) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "detect",
		Short:  "Detect language, framework, and recommended SDK for a project",
		Hidden: true,
		RunE:   runDetect(detector),
	}

	cmd.Flags().String(pathFlag, "", "Path to the project directory (defaults to current directory)")

	return cmd
}

func runDetect(detector setup.Detector) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString(pathFlag)
		if dir == "" {
			var err error
			dir, err = os.Getwd()
			if err != nil {
				return err
			}
		}

		result, err := detector.Detect(dir)
		if err != nil {
			return err
		}

		outputKind := cliflags.GetOutputKind(cmd)
		if outputKind == "json" {
			data, _ := json.Marshal(result)
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Language: %s\n", result.Language)
		if result.Framework != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Framework: %s\n", result.Framework)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Package Manager: %s\n", result.PackageManager)
		fmt.Fprintf(cmd.OutOrStdout(), "Recommended SDK: %s\n", result.SDKID)
		fmt.Fprintf(cmd.OutOrStdout(), "Entry Point: %s\n", result.EntryPoint)

		return nil
	}
}

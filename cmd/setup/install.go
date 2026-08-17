package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	"github.com/launchdarkly/ldcli/internal/setup"
)

const (
	sdkIDFlag  = "sdk-id"
	dryRunFlag = "dry-run"
)

func newInstallCmd(svc setup.Service) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "install",
		Short:  "Install the LaunchDarkly SDK package for the detected project",
		Hidden: true,
		RunE:   runInstall(svc),
	}

	cmd.Flags().String(pathFlag, "", "Path to the project directory (defaults to current directory)")
	cmd.Flags().String(sdkIDFlag, "", "SDK identifier to install (e.g. node-server, react-client-sdk)")
	_ = cmd.MarkFlagRequired(sdkIDFlag)
	cmd.Flags().String("package-manager", "", "Package manager to use (e.g. npm, pip, go)")
	cmd.Flags().Bool(dryRunFlag, false, "Print the install command that would run without executing it")

	return cmd
}

// candidateList renders the choices for an error message, marking the ones that
// are not installed so the user isn't sent to a tool they'd have to install first.
func candidateList(candidates []setup.PMCandidate) string {
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c.Installed {
			names = append(names, c.Name)
			continue
		}
		names = append(names, c.Name+" (not installed)")
	}
	return strings.Join(names, ", ")
}

func runInstall(svc setup.Service) func(*cobra.Command, []string) error {
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
		dryRun, _ := cmd.Flags().GetBool(dryRunFlag)

		// Without an explicit choice, read the project rather than falling back to
		// pip or npm regardless of what the project uses. An ambiguous project is an
		// error: guessing here would install with the wrong manager, and this command
		// cannot ask.
		if pkgMgr == "" {
			choice := setup.PackageManagerChoiceFor(dir, sdkID)
			if choice.Confidence == setup.PMAmbiguous {
				return fmt.Errorf(
					"cannot tell which package manager to use: %s\npass --package-manager with one of: %s",
					choice.Reason, candidateList(choice.Candidates),
				)
			}
			pkgMgr = choice.Name
		}

		detection := &setup.DetectResult{
			SDKID:          sdkID,
			PackageManager: pkgMgr,
		}

		var result *setup.InstallResult
		if dryRun {
			args, pkg := setup.InstallArgs(dir, sdkID, pkgMgr)
			result = &setup.InstallResult{
				SDKID:   sdkID,
				Package: pkg,
				Command: strings.Join(args, " "),
				DryRun:  true,
			}
		} else {
			var err error
			result, err = svc.Install(dir, detection)
			if err != nil {
				return err
			}
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
		if result.AlreadyInstalled {
			fmt.Fprintln(cmd.OutOrStdout(), "Already installed — skipping install.")
			return nil
		}
		if result.Command != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Command: %s\n", result.Command)
		}
		if result.DryRun {
			fmt.Fprintln(cmd.OutOrStdout(), "Dry run: command not executed")
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Success: %t\n", result.Success)
		if result.Warning != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s\n", result.Warning)
		}
		switch {
		case result.FailureReason != "":
			fmt.Fprintf(cmd.OutOrStdout(), "Reason: %s\n", result.FailureReason)
		case !result.Success && setup.RequiresManualInstall(result.SDKID):
			fmt.Fprintf(cmd.OutOrStdout(), "Reason: %s has no automated install command; add %s to your build configuration by hand.\n", result.SDKID, result.Package)
		}

		return nil
	}
}

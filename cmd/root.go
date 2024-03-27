package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/environments"
	flagscmd "ldcli/cmd/flags"
	mbrscmd "ldcli/cmd/members"
	projcmd "ldcli/cmd/projects"
	"ldcli/internal/flags"
	"ldcli/internal/members"
	"ldcli/internal/projects"
)

func NewRootCommand(flagsClient flags.Client, membersClient members.Client, projectsClient projects.Client) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:     "ldcli",
		Short:   "LaunchDarkly CLI",
		Long:    "LaunchDarkly CLI to control your feature flags",
		Version: "0.0.1", // TODO: set this based on release or use `cmd.SetVersionTemplate(s string)`

		// Handle errors differently based on type.
		// We don't want to show the usage if the user has the right structure but invalid data such as
		// the wrong key.
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.PersistentFlags().StringP(
		"accessToken",
		"t",
		"",
		"LaunchDarkly personal access token",
	)
	err := cmd.MarkPersistentFlagRequired("accessToken")
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag("accessToken", cmd.PersistentFlags().Lookup("accessToken"))
	if err != nil {
		return nil, err
	}

	cmd.PersistentFlags().StringP(
		"baseUri",
		"u",
		"https://app.launchdarkly.com",
		"LaunchDarkly base URI",
	)
	err = viper.BindPFlag("baseUri", cmd.PersistentFlags().Lookup("baseUri"))
	if err != nil {
		return nil, err
	}

	flagsCmd, err := flagscmd.NewFlagsCmd(flagsClient)
	if err != nil {
		return nil, err
	}
	environmentsCmd, err := environments.NewEnvironmentsCmd()
	if err != nil {
		return nil, err
	}
	membersCmd, err := mbrscmd.NewMembersCmd(membersClient)
	if err != nil {
		return nil, err
	}
	projectsCmd, err := projcmd.NewProjectsCmd(projectsClient)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(NewQuickStartCmd(flagsClient))
	cmd.AddCommand(flagsCmd)
	cmd.AddCommand(environmentsCmd)
	cmd.AddCommand(membersCmd)
	cmd.AddCommand(projectsCmd)
	cmd.AddCommand(setupCmd)

	return cmd, nil
}

func Execute() {
	rootCmd, err := NewRootCommand(flags.NewClient(), members.NewClient(), projects.NewClient())
	if err != nil {
		log.Fatal(err)
	}

	err = rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
}

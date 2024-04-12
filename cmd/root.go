package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	envscmd "ldcli/cmd/environments"
	flagscmd "ldcli/cmd/flags"
	mbrscmd "ldcli/cmd/members"
	projcmd "ldcli/cmd/projects"
	"ldcli/internal/analytics"
	"ldcli/internal/environments"
	"ldcli/internal/flags"
	"ldcli/internal/members"
	"ldcli/internal/projects"
)

func NewRootCommand(
	analyticsTracker analytics.Tracker,
	environmentsClient environments.Client,
	flagsClient flags.Client,
	membersClient members.Client,
	projectsClient projects.Client,
	version string,
	useConfigFile bool,
) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:     "ldcli",
		Short:   "LaunchDarkly CLI",
		Long:    "LaunchDarkly CLI to control your feature flags",
		Version: version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// disable required flags when running certain commands, not a flag
			if cmd.Name() == "help" || cmd.Parent().Name() == "completion" {
				cmd.DisableFlagParsing = true
			}
		},

		// Handle errors differently based on type.
		// We don't want to show the usage if the user has the right structure but invalid data such as
		// the wrong key.
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	if useConfigFile {
		err := setupFlagsFromConfig()
		if err != nil {
			return nil, err
		}
	}

	cmd.PersistentFlags().String(
		cliflags.AccessTokenFlag,
		"",
		"LaunchDarkly API token with write-level access",
	)
	err := cmd.MarkPersistentFlagRequired(cliflags.AccessTokenFlag)
	if err != nil {
		return nil, err
	}
	err = viper.BindPFlag(cliflags.AccessTokenFlag, cmd.PersistentFlags().Lookup(cliflags.AccessTokenFlag))
	if err != nil {
		return nil, err
	}

	cmd.PersistentFlags().String(
		cliflags.BaseURIFlag,
		"https://app.launchdarkly.com",
		"LaunchDarkly base URI",
	)
	err = viper.BindPFlag(cliflags.BaseURIFlag, cmd.PersistentFlags().Lookup(cliflags.BaseURIFlag))
	if err != nil {
		return nil, err
	}

	environmentsCmd, err := envscmd.NewEnvironmentsCmd(analyticsTracker, environmentsClient)
	if err != nil {
		return nil, err
	}
	flagsCmd, err := flagscmd.NewFlagsCmd(flagsClient)
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

	cmd.AddCommand(environmentsCmd)
	cmd.AddCommand(flagsCmd)
	cmd.AddCommand(membersCmd)
	cmd.AddCommand(projectsCmd)
	cmd.AddCommand(NewQuickStartCmd(environmentsClient, flagsClient))

	return cmd, nil
}

func Execute(analyticsTracker analytics.Tracker, version string) {
	rootCmd, err := NewRootCommand(
		analyticsTracker,
		environments.NewClient(version),
		flags.NewClient(version),
		members.NewClient(version),
		projects.NewClient(version),
		version,
		true,
	)
	if err != nil {
		log.Fatal(err)
	}

	err = rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
}

func setupFlagsFromConfig() error {
	configPath := os.Getenv("XDG_CONFIG_HOME")
	if configPath == "" {
		home, err := homedir.Dir()
		if err != nil {
			return err
		}
		configPath = filepath.Join(home, ".config")
	}
	configPath = filepath.Join(configPath, "ldcli")

	viper.AddConfigPath(configPath)
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// ignore if file not found
		} else {
			return err
		}
	}

	return nil
}

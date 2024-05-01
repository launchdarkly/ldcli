package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	configcmd "ldcli/cmd/config"
	envscmd "ldcli/cmd/environments"
	flagscmd "ldcli/cmd/flags"
	mbrscmd "ldcli/cmd/members"
	projcmd "ldcli/cmd/projects"
	resourcecmd "ldcli/cmd/resources"
	"ldcli/internal/analytics"
	"ldcli/internal/config"
	"ldcli/internal/environments"
	"ldcli/internal/flags"
	"ldcli/internal/members"
	"ldcli/internal/projects"
	"ldcli/internal/resources"
)

type APIClients struct {
	EnvironmentsClient environments.Client
	FlagsClient        flags.Client
	MembersClient      members.Client
	ProjectsClient     projects.Client
	ResourcesClient    resources.Client
}

func NewRootCommand(
	analyticsTracker analytics.Tracker,
	clients APIClients,
	version string,
	useConfigFile bool,
) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:     "ldcli",
		Short:   "LaunchDarkly CLI",
		Long:    "LaunchDarkly CLI to control your feature flags",
		Version: version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// disable required flags when running certain commands
			for _, name := range []string{
				"completion",
				"config",
				"help",
			} {
				if cmd.HasParent() && cmd.Parent().Name() == name {
					cmd.DisableFlagParsing = true
				}
				if cmd.Name() == name {
					cmd.DisableFlagParsing = true
				}
			}
		},
		// Handle errors differently based on type.
		// We don't want to show the usage if the user has the right structure but invalid data such as
		// the wrong key.
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	if useConfigFile {
		setFlagsFromConfig()
	}

	viper.SetEnvPrefix("LD")
	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.AutomaticEnv()

	cmd.PersistentFlags().String(
		cliflags.AccessTokenFlag,
		"",
		cliflags.AccessTokenFlagDescription,
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
		cliflags.BaseURIDefault,
		cliflags.BaseURIFlagDescription,
	)
	err = viper.BindPFlag(cliflags.BaseURIFlag, cmd.PersistentFlags().Lookup(cliflags.BaseURIFlag))
	if err != nil {
		return nil, err
	}

	cmd.PersistentFlags().Bool(
		cliflags.AnalyticsOptOut,
		false,
		cliflags.AnalyticsOptOutDescription,
	)
	err = viper.BindPFlag(cliflags.AnalyticsOptOut, cmd.PersistentFlags().Lookup(cliflags.AnalyticsOptOut))
	if err != nil {
		return nil, err
	}

	cmd.PersistentFlags().StringP(
		cliflags.OutputFlag,
		"o",
		"plaintext",
		cliflags.OutputFlagDescription,
	)
	err = viper.BindPFlag(cliflags.OutputFlag, cmd.PersistentFlags().Lookup(cliflags.OutputFlag))
	if err != nil {
		return nil, err
	}

	environmentsCmd, err := envscmd.NewEnvironmentsCmd(analyticsTracker, clients.EnvironmentsClient)
	if err != nil {
		return nil, err
	}
	flagsCmd, err := flagscmd.NewFlagsCmd(analyticsTracker, clients.FlagsClient)
	if err != nil {
		return nil, err
	}
	membersCmd, err := mbrscmd.NewMembersCmd(analyticsTracker, clients.MembersClient)
	if err != nil {
		return nil, err
	}
	projectsCmd, err := projcmd.NewProjectsCmd(analyticsTracker, clients.ProjectsClient)
	if err != nil {
		return nil, err
	}

	cmd.AddCommand(configcmd.NewConfigCmd(analyticsTracker))
	cmd.AddCommand(environmentsCmd)
	cmd.AddCommand(flagsCmd)
	cmd.AddCommand(membersCmd)
	cmd.AddCommand(projectsCmd)
	cmd.AddCommand(NewQuickStartCmd(analyticsTracker, clients.EnvironmentsClient, clients.FlagsClient))

	resourcecmd.AddAllResourceCmds(cmd, clients.ResourcesClient, analyticsTracker)

	return cmd, nil
}

func Execute(analyticsTracker analytics.Tracker, version string) {
	clients := APIClients{
		EnvironmentsClient: environments.NewClient(version),
		FlagsClient:        flags.NewClient(version),
		MembersClient:      members.NewClient(version),
		ProjectsClient:     projects.NewClient(version),
		ResourcesClient:    resources.NewClient(version),
	}
	rootCmd, err := NewRootCommand(
		analyticsTracker,
		clients,
		version,
		true,
	)
	if err != nil {
		log.Fatal(err)
	}

	err = rootCmd.Execute()
	outcome := analytics.SUCCESS
	if err != nil {
		outcome = analytics.ERROR
		fmt.Fprintln(os.Stderr, err.Error())
	}

	analyticsTracker.SendCommandCompletedEvent(
		viper.GetString(cliflags.AccessTokenFlag),
		viper.GetString(cliflags.BaseURIDefault),
		viper.GetBool(cliflags.AnalyticsOptOut),
		outcome,
	)
}

// setFlagsFromConfig reads in the config file if it exists and uses any flag values for commands.
func setFlagsFromConfig() {
	viper.SetConfigFile(config.GetConfigFile())
	_ = viper.ReadInConfig()
}

//go:generate go run resources/gen_resources.go

package cmd

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"ldcli/cmd/cliflags"
	configcmd "ldcli/cmd/config"
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

	cmd.AddCommand(configcmd.NewConfigCmd(analyticsTracker))
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

	optOutStr := rootCmd.PersistentFlags().Lookup(cliflags.AnalyticsOptOut).Value.String()
	optOut, _ := strconv.ParseBool(optOutStr)
	analyticsTracker.SendCommandCompletedEvent(
		rootCmd.PersistentFlags().Lookup(cliflags.AccessTokenFlag).Value.String(),
		rootCmd.PersistentFlags().Lookup(cliflags.BaseURIFlag).Value.String(),
		optOut,
		outcome,
	)
}

// setFlagsFromConfig reads in the config file if it exists and uses any flag values for commands.
func setFlagsFromConfig() {
	viper.SetConfigFile(config.GetConfigFile())
	_ = viper.ReadInConfig()
}

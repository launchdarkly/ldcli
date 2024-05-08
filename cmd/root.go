//go:generate go run resources/gen_resources.go

package cmd

import (
	"fmt"
	mbrscmd "ldcli/cmd/members"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "ldcli/cmd/analytics"
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

type Command interface {
	Cmd() *cobra.Command
	HelpCalled() bool
}

type RootCmd struct {
	Commands   []Command
	cmd        *cobra.Command
	helpCalled bool
}

func (cmd RootCmd) Cmd() *cobra.Command {
	return cmd.cmd
}

func (cmd RootCmd) HelpCalled() bool {
	for _, c := range cmd.Commands {
		if c.HelpCalled() {
			return true
		}
	}

	return cmd.helpCalled
}

func (cmd RootCmd) Execute() error {
	return cmd.cmd.Execute()
}

func NewRootCommand(
	analyticsTrackerFn analytics.TrackerFn,
	clients APIClients,
	version string,
	useConfigFile bool,
) (*RootCmd, error) {
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
	rootCmd := &RootCmd{
		cmd: cmd,
	}

	hf := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		rootCmd.helpCalled = true

		// get the resource for the tracking event, not the action
		resourceCommand := getResourceCommand(c)
		analyticsTrackerFn(
			viper.GetString(cliflags.AccessTokenFlag),
			viper.GetString(cliflags.BaseURIFlag),
			viper.GetBool(cliflags.AnalyticsOptOut),
		).SendCommandRunEvent(
			cmdAnalytics.CmdRunEventProperties(c,
				resourceCommand.Name(),
				map[string]interface{}{
					"action": "help",
				},
			),
		)
		hf(c, args)
	})

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

	membersCmd, err := mbrscmd.NewMembersCmd(analyticsTrackerFn, clients.MembersClient)
	if err != nil {
		return nil, err
	}

	configCmd := configcmd.NewConfigCmd(analyticsTrackerFn)
	cmd.AddCommand(configCmd.Cmd())
	cmd.AddCommand(NewQuickStartCmd(analyticsTrackerFn, clients.EnvironmentsClient, clients.FlagsClient))
	cmd.AddCommand(membersCmd)
	resourcecmd.AddAllResourceCmds(cmd, clients.ResourcesClient, analyticsTrackerFn)

	rootCmd.Commands = append(rootCmd.Commands, configCmd)

	return rootCmd, nil
}

func Execute(version string) {
	clients := APIClients{
		EnvironmentsClient: environments.NewClient(version),
		FlagsClient:        flags.NewClient(version),
		MembersClient:      members.NewClient(version),
		ProjectsClient:     projects.NewClient(version),
		ResourcesClient:    resources.NewClient(version),
	}
	trackerFn := analytics.ClientFn{
		ID: uuid.New().String(),
	}
	rootCmd, err := NewRootCommand(
		trackerFn.Tracker(version),
		clients,
		version,
		true,
	)
	if err != nil {
		log.Fatal(err)
	}

	err = rootCmd.Execute()

	var outcome string
	switch {
	case rootCmd.HelpCalled():
		outcome = analytics.HELP
	case err != nil:
		outcome = analytics.ERROR
		fmt.Fprintln(os.Stderr, err.Error())
	default:
		outcome = analytics.SUCCESS
	}

	analyticsClient := trackerFn.Tracker(version)(
		rootCmd.cmd.PersistentFlags().Lookup(cliflags.AccessTokenFlag).Value.String(),
		rootCmd.cmd.PersistentFlags().Lookup(cliflags.BaseURIFlag).Value.String(),
		viper.GetBool(cliflags.AnalyticsOptOut),
	)
	analyticsClient.SendCommandCompletedEvent(outcome)
	analyticsClient.Wait()
}

// setFlagsFromConfig reads in the config file if it exists and uses any flag values for commands.
func setFlagsFromConfig() {
	viper.SetConfigFile(config.GetConfigFile())
	_ = viper.ReadInConfig()
}

// getResourceCommand returns the command for a resource or an action's parent resource.
// ldcli projects // returns projects command
// ldcli projects list // returns projects command
func getResourceCommand(c *cobra.Command) *cobra.Command {
	if !c.HasParent() || c.Parent() == c.Root() {
		return c
	}

	return getResourceCommand(c.Parent())
}

//go:generate go run resources/gen_resources.go

package cmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmdAnalytics "github.com/launchdarkly/ldcli/cmd/analytics"
	"github.com/launchdarkly/ldcli/cmd/cliflags"
	configcmd "github.com/launchdarkly/ldcli/cmd/config"
	flagscmd "github.com/launchdarkly/ldcli/cmd/flags"
	logincmd "github.com/launchdarkly/ldcli/cmd/login"
	memberscmd "github.com/launchdarkly/ldcli/cmd/members"
	resourcecmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/internal/analytics"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/environments"
	errs "github.com/launchdarkly/ldcli/internal/errors"
	"github.com/launchdarkly/ldcli/internal/flags"
	"github.com/launchdarkly/ldcli/internal/members"
	"github.com/launchdarkly/ldcli/internal/projects"
	"github.com/launchdarkly/ldcli/internal/resources"
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
	configService config.Service,
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
				"login",
			} {
				if cmd.HasParent() && cmd.Parent().Name() == name {
					cmd.DisableFlagParsing = true
				}
				if cmd.Name() == name {
					cmd.DisableFlagParsing = true
				}
			}
		},
		Annotations: make(map[string]string),
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
		err := setFlagsFromConfig()
		if err != nil {
			return nil, err
		}
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

	configCmd := configcmd.NewConfigCmd(configService, analyticsTrackerFn)
	cmd.AddCommand(configCmd.Cmd())
	cmd.AddCommand(NewQuickStartCmd(analyticsTrackerFn, clients.EnvironmentsClient, clients.FlagsClient))
	cmd.AddCommand(logincmd.NewLoginCmd(resources.NewClient(version)))
	cmd.AddCommand(resourcecmd.NewResourcesCmd())
	resourcecmd.AddAllResourceCmds(cmd, clients.ResourcesClient, analyticsTrackerFn)

	// add non-generated commands
	for _, c := range cmd.Commands() {
		if c.Name() == "flags" {
			c.AddCommand(flagscmd.NewToggleOnCmd(clients.ResourcesClient))
			c.AddCommand(flagscmd.NewToggleOffCmd(clients.ResourcesClient))
			c.AddCommand(flagscmd.NewArchiveCmd(clients.ResourcesClient))
		}
		if c.Name() == "members" {
			c.AddCommand(memberscmd.NewMembersInviteCmd(clients.ResourcesClient))
		}
	}

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
	configService := config.NewService(resources.NewClient(version))
	trackerFn := analytics.ClientFn{
		ID: uuid.New().String(),
	}
	rootCmd, err := NewRootCommand(
		configService,
		trackerFn.Tracker(version),
		clients,
		version,
		true,
	)
	if err != nil {
		log.Fatal(err)
	}

	// change the completion command help
	rootCmd.Cmd().InitDefaultCompletionCmd()
	completionCmd, _, err := rootCmd.Cmd().Find([]string{"completion"})
	if err == nil {
		completionCmd.Long = fmt.Sprintf(`Generate the autocompletion script for %[1]s for the specified shell.
See each command's help for details on how to use the generated script.`, rootCmd.Cmd().Name())
		completionCmd.SetUsageTemplate(resourcecmd.SubcommandUsageTemplate())
		rootCmd.Cmd().AddCommand(completionCmd)
	}

	cobra.AddTemplateFunc("WrappedRequiredFlagUsages", WrappedRequiredFlagUsages)
	cobra.AddTemplateFunc("WrappedOptionalFlagUsages", WrappedOptionalFlagUsages)
	cobra.AddTemplateFunc("HasRequiredFlags", HasRequiredFlags)
	cobra.AddTemplateFunc("HasOptionalFlags", HasOptionalFlags)
	rootCmd.cmd.SetUsageTemplate(getUsageTemplate())

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
		viper.GetString(cliflags.AccessTokenFlag),
		viper.GetString(cliflags.BaseURIFlag),
		viper.GetBool(cliflags.AnalyticsOptOut),
	)
	if err != nil {
		// If there's an error, it could be because of a missing flag. In that case, don't send
		// analytics.
		if errors.Is(err, errs.Error{}) {
			analyticsClient.SendCommandCompletedEvent(outcome)
		}
	} else {
		analyticsClient.SendCommandCompletedEvent(outcome)
	}

	analyticsClient.Wait()
}

// setFlagsFromConfig reads in the config file if it exists and uses any flag values for commands.
func setFlagsFromConfig() error {
	configFile := config.GetConfigFile()
	viper.SetConfigType("yml")
	viper.SetConfigFile(configFile)

	err := makePath(configFile)
	if err != nil {
		return err
	}

	_ = viper.ReadInConfig()

	return nil
}

func makePath(path string) error {
	dir := filepath.Dir(path)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return err
		}
	}

	return nil
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

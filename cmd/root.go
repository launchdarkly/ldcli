package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	projcmd "ld-cli/cmd/projects"
	errs "ld-cli/internal/errors"
	"ld-cli/internal/projects"
)

type rootCmd struct {
	Cmd *cobra.Command
}

var rootCmd2 = &cobra.Command{
	Use:     "ldcli",
	Short:   "LaunchDarkly CLI",
	Long:    "LaunchDarkly CLI to control your feature flags",
	Version: "0.0.1", // TODO: set this based on release or use `cmd.SetVersionTemplate(s string)`

	// Handle errors differently based on type.
	// We don't want to show the usage if the user has the right structure but invalid data such as
	// the wrong key.
	SilenceUsage:  true,
	SilenceErrors: true,
}

// func newRootCommand(projectClient projects.Client) *cobra.Command {
func NewRootCmd(clientFn projects.ProjectsClientFn) (rootCmd, error) {
	// cobra.OnInitialize(RebindKeys)
	cmd := &cobra.Command{
		Use:     "ldcli",
		Short:   "LaunchDarkly CLI",
		Long:    "LaunchDarkly CLI to control your feature flags",
		Version: "0.0.1", // TODO: set this based on release or use `cmd.SetVersionTemplate(s string)`

		// Handle errors differently based on type.
		// We don't want to show the usage if the user has the right structure but invalid data such as
		// the wrong key.
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringP(
		"accessToken",
		"t",
		"",
		"LaunchDarkly personal access token",
	)
	err := cmd.MarkPersistentFlagRequired("accessToken")
	if err != nil {
		return rootCmd{}, err
	}
	err = viper.BindPFlag("accessToken", cmd.PersistentFlags().Lookup("accessToken"))
	if err != nil {
		return rootCmd{}, err
	}

	// cmd.PersistentFlags().StringP(
	// 	"baseUri",
	// 	"u",
	// 	"https://app.launchdarkly.com",
	// 	"LaunchDarkly base URI",
	// )
	// err = cmd.MarkPersistentFlagRequired("baseUri")
	// if err != nil {
	// 	return rootCmd{}, err
	// }
	// err = viper.BindPFlag("baseUri", cmd.PersistentFlags().Lookup("baseUri"))
	// if err != nil {
	// 	return rootCmd{}, err
	// }
	// viper.SetDefault("baseUri", "https://app.launchdarkly.com")
	// f, e := cmd.PersistentFlags().GetString("baseUri")
	fmt.Println(
		">>> baseUri",
		viper.GetString("baseUri"),
		// "::",
		// cmd.PersistentFlags().Lookup("baseUri").Value.String(),
	)

	// err = cmd.PersistentFlags().Parse([]string{"baseUri"})
	projectClient := clientFn(viper.GetString("accessToken"), viper.GetString("baseUri"))
	projectsCmd := projcmd.NewProjectsCmd(projectClient)

	cmd.AddCommand(projectsCmd)
	cmd.AddCommand(setupCmd)

	return rootCmd{
		Cmd: cmd,
	}, nil
}

func init() {
	rootCmd2.PersistentFlags().StringP(
		"baseUri",
		"u",
		"https://app.launchdarkly.com",
		"LaunchDarkly base URI",
	)
	err := rootCmd2.MarkPersistentFlagRequired("baseUri")
	if err != nil {
		panic(err)
	}
	err = viper.BindPFlag("baseUri", rootCmd2.PersistentFlags().Lookup("baseUri"))
	if err != nil {
		panic(err)
	}
	viper.SetDefault("baseUri", "https://app.launchdarkly.com")
	f, e := rootCmd2.PersistentFlags().GetString("baseUri")
	fmt.Println(
		">>> baseUri",
		viper.GetString("baseUri"),
		"::",
		rootCmd2.PersistentFlags().Lookup("baseUri").Value.String(),
		"::",
		f,
		e,
	)

	rootCmd2.PersistentFlags().Parse([]string{"baseUri"})
}

func Execute() {
	// rootCmd, err := NewRootCmd(projects.NewClient)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// err = rootCmd.Cmd.Execute()
	err := rootCmd2.Execute()
	if err != nil {
		switch {
		case errors.Is(err, errs.ErrForbidden),
			errors.Is(err, errs.ErrInvalidBaseURI),
			errors.Is(err, errs.ErrUnauthorized):
			fmt.Fprintln(os.Stderr, err.Error())
		default:
			fmt.Println(err.Error())
			// fmt.Println(rootCmd.Cmd.UsageString())
			fmt.Println(rootCmd2.UsageString())
		}
	}
}

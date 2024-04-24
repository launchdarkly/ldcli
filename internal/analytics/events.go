package analytics

import (
	"ldcli/cmd/cliflags"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func CmdRunEventProperties(cmd *cobra.Command, name string) map[string]interface{} {
	baseURI := viper.GetString(cliflags.BaseURIFlag)
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, f.Name)
	})

	properties := map[string]interface{}{
		"name":   name,
		"action": cmd.CalledAs(),
		"flags":  flags,
	}
	if baseURI != cliflags.BaseURIDefault {
		properties["baseURI"] = baseURI
	}
	return properties
}

const (
	SUCCESS = "success"
	ERROR   = "error"
	HELP    = "help"
)

func MockedTracker(name string, action string, flags []string, outcome string) *MockTracker {
	id := "test-id"
	tracker := MockTracker{ID: id}
	tracker.On("SendEvent", []interface{}{
		"testAccessToken",
		"http://test.com",
		"CLI Command Run",
		map[string]interface{}{
			"action":  action,
			"baseURI": "http://test.com",
			"flags":   flags,
			"id":      id,
			"name":    name,
		},
	}...)
	tracker.On("SendEvent", []interface{}{
		"testAccessToken",
		"http://test.com",
		"CLI Command Completed",
		map[string]interface{}{
			"id":      id,
			"outcome": outcome,
		},
	}...)
	return &tracker
}

func Outcome(cmd *cobra.Command) string {
	outcome := SUCCESS

	if _, hasErr := cmd.Annotations["err"]; hasErr {
		outcome = ERROR
	}
	if cmd.HasHelpSubCommands() {
		outcome = HELP
	}

	return outcome
}

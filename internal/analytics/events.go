package analytics

import (
	"ldcli/cmd/cliflags"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func CmdRunEventProperties(cmd *cobra.Command, name string) map[string]interface{} {
	id := uuid.New()
	baseURI := viper.GetString(cliflags.BaseURIFlag)
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flags = append(flags, f.Name)
	})

	properties := map[string]interface{}{
		"name":   name,
		"action": cmd.CalledAs(),
		"flags":  flags,
		"id":     id.String(),
	}
	if baseURI != cliflags.BaseURIDefault {
		properties["baseURI"] = baseURI
	}
	return properties
}

func MockedTracker(name string, action string, flags []string) (*MockTracker, []interface{}) {
	id := "test-id"
	mockedTrackingArgs := []interface{}{
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
	}
	tracker := MockTracker{ID: id}
	tracker.On("SendEvent", mockedTrackingArgs...)
	return &tracker, mockedTrackingArgs
}

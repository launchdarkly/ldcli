package evaluate

import (
	"fmt"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	ldclient "github.com/launchdarkly/go-server-sdk/v7"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"os"
	"slices"
	"strings"
)

const example = `  # Using JSON directly
  %[1]s %[2]s string my-flag-key --context '{"key": "user-123", "kind": "user", "name": "John Doe"}'

  # Using raw field key-value pairs (always strings)
  %[1]s %[2]s string my-flag-key -F key=user-123 -F kind=user -F premium=true -F age=25

  # Using field key-value pairs with type conversion
  %[1]s %[2]s string my-flag-key -f key=user-123 -f kind=user -f premium=true

  # Reading from stdin
  echo '{"kind": "user", "key": "user-123"}' | %[1]s %[2]s string my-flag-key --context @-

  # Reading from file
  %[1]s %[2]s string my-flag-key --context @ldContextFlag.json

  # Multiple contexts
  echo '{"kind": "user", "key": "user-123", "user": "user123"}' | %[1]s %[2]s my-setter --context @- -f key=another -f kind=device -f key=somethingelse -f kind=anotherkind`

func NewEvaluateCommand(parent *cobra.Command) *cobra.Command {
	contextFlag := newContextFlag(os.Stdin)

	cmd := &cobra.Command{
		Use:   "evaluate TYPE FLAG-KEY",
		Short: "Evaluate a LaunchDarkly feature setter with an arbitrary context",
		Long: `Evaluate a LaunchDarkly feature setter using a provided context in JSON format.

Each JSON based context must be a complete context.

When providing contexts via the field arguments, you must specify a key as the first item. Additional contexts can be added by specifying a new key and kind.`,
		Args: validArgs,
		RunE: getEvaluateFlagFn(contextFlag),
	}
	cmd.Example = fmt.Sprintf(example, parent.CommandPath(), cmd.CommandPath())
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	cmd.Flags().Var(contextFlag.jsonValue(), "context", "Context JSON for evaluation (use @- for stdin, @filename for file)")
	cmd.Flags().VarP(contextFlag.magicValue(), "field", "F", "Add field in key=value format with type conversion")
	cmd.Flags().VarP(contextFlag.rawValue(), "raw-field", "f", "Add field in key=value format as raw strings")
	cmd.Flags().StringP("profile", "p", "default", "The profile to use for evaluation")

	return cmd
}

func validArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(2)(cmd, args); err != nil {
		return err
	}
	validVariations := []string{"int", "bool", "float", "string", "json"}
	if !slices.Contains(validVariations, strings.ToLower(args[0])) {
		return fmt.Errorf("invalid variation type %q, must be one of %s", args[0], strings.Join(validVariations, ", "))
	}

	return nil
}

func getEvaluateFlagFn(contextFlag *ldContextFlag) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		variationType := strings.ToLower(args[0])
		flagKey := args[1]

		ldContext, err := contextFlag.ldContext()
		if err != nil {
			return err
		}

		client, err := getSDKClient(cmd.Flags())
		if err != nil {
			return fmt.Errorf("failed to create LaunchDarkly client: %v", err)
		}
		defer client.Close()

		var result interface{}
		switch variationType {
		case "bool":
			result, err = client.BoolVariation(flagKey, ldContext, false)
		case "int":
			result, err = client.IntVariation(flagKey, ldContext, 0)
		case "float":
			result, err = client.Float64Variation(flagKey, ldContext, 0)
		case "string":
			result, err = client.StringVariation(flagKey, ldContext, "")
		case "json":
			result, err = client.JSONVariation(flagKey, ldContext, ldvalue.String(""))
		}

		if err != nil {
			return fmt.Errorf("failed to evaluate flag: %w", err)
		}

		fmt.Printf("Flag %q evaluation result: %v\n", flagKey, result)

		return nil
	}
}

func getSDKClient(fs *pflag.FlagSet) (*ldclient.LDClient, error) {
	// todo need to bind the profile flag to viper, though it doesn't make sense to do so
	//  since the profile flag is only used here. maybe there's an opportunity to use it
	//  across the whole cli?
	//profile := viper.GetString("environments")

	profile, err := fs.GetString("profile")
	if err != nil {
		return nil, err
	}
	cfg := viper.GetStringMap("environments." + profile)
	_ = cfg

	// todo create ldclient. neither streaming nor polling is a that great,
	//  since that retrieves all flag data for every evaluation.
	//  potential options:
	//  - cache locally. will the sdk update incrementally?
	//  - maybe: https://launchdarkly.com/docs/api/contexts/evaluate-context-instance?
	//    we'd just need the api key, but this won't work for the dev-server

	return nil, errors.New("")
}

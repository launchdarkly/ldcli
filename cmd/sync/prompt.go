package sync

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"

	"github.com/launchdarkly/ldcli/cmd/cliflags"
	resourcescmd "github.com/launchdarkly/ldcli/cmd/resources"
	"github.com/launchdarkly/ldcli/cmd/validators"
	"github.com/launchdarkly/ldcli/internal/config"
	"github.com/launchdarkly/ldcli/internal/output"
	"github.com/launchdarkly/ldcli/internal/resources"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

const (
	addFlag    = "add"
	applyFlag  = "apply"
	dryRunFlag = "dry-run"
	yesFlag    = "yes"
)

type bootstrapRunner func(syncbootstrap.Options) error

func NewPromptCmd(client resources.Client) *cobra.Command {
	return newPromptCmd(client, syncbootstrap.Run)
}

func newPromptCmd(
	client resources.Client,
	bootstrap bootstrapRunner,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Synchronize local prompt variations with LaunchDarkly",
		Long:  "Bootstrap local prompt variations from LaunchDarkly, add more variations, preview synchronization changes, or apply a durable sync plan.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}

			return validators.Validate()(cmd, args)
		},
		RunE: runPrompt(client, bootstrap),
	}

	cmd.Flags().Bool(
		addFlag,
		false,
		"Select additional prompt variations from LaunchDarkly",
	)
	cmd.Flags().Bool(
		dryRunFlag,
		false,
		"Preview synchronization changes without creating a plan",
	)
	cmd.Flags().String(
		applyFlag,
		"",
		"Apply an existing durable plan ID without planning again",
	)
	cmd.Flags().Bool(
		yesFlag,
		false,
		"Apply planned changes without interactive confirmation",
	)
	cmd.Flags().String(
		cliflags.ProjectFlag,
		"",
		"Project key for --apply when it cannot be inferred from the workspace",
	)
	cmd.SetUsageTemplate(resourcescmd.SubcommandUsageTemplate())

	return cmd
}

func runPrompt(
	client resources.Client,
	bootstrap bootstrapRunner,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		resolver := syncsource.NewResolver(config.GetConfigFile())
		root, err := resolver.ResolveRoot(cwd)
		if err != nil {
			return err
		}

		accessToken := viper.GetString(cliflags.AccessTokenFlag)
		baseURI := viper.GetString(cliflags.BaseURIFlag)
		store := synclocal.NewStore(root)

		storeExists, err := store.Exists()
		if err != nil {
			return err
		}
		add, _ := cmd.Flags().GetBool(addFlag)
		dryRun, _ := cmd.Flags().GetBool(dryRunFlag)
		planID, _ := cmd.Flags().GetString(applyFlag)
		yes, _ := cmd.Flags().GetBool(yesFlag)
		if planID != "" {
			if dryRun || add {
				return fmt.Errorf("--apply cannot be used with --dry-run or --add")
			}
			if _, err := uuid.Parse(planID); err != nil {
				return fmt.Errorf("invalid plan ID %q: %w", planID, err)
			}
			projectKey, err := applyProjectKey(cmd, store, storeExists)
			if err != nil {
				return err
			}
			result, err := syncapi.NewClient(client).Apply(
				accessToken,
				baseURI,
				projectKey,
				planID,
				nil,
			)
			if err != nil {
				return output.NewCmdOutputError(
					err,
					cliflags.GetOutputKind(cmd),
				)
			}
			return writeSyncOutput(
				cmd.OutOrStdout(),
				cliflags.GetOutputKind(cmd),
				nil,
				[]syncapi.ProjectApply{result},
			)
		}
		if !storeExists || add {
			err := bootstrap(syncbootstrap.Options{
				Catalog: syncapi.NewCatalogClient(
					client,
					accessToken,
					baseURI,
				),
				Store:   store,
				Input:   cmd.InOrStdin(),
				Output:  cmd.OutOrStdout(),
				Initial: !storeExists,
				DryRun:  dryRun,
			})
			if err != nil {
				return output.NewCmdOutputError(
					err,
					cliflags.GetOutputKind(cmd),
				)
			}

			return nil
		}

		workspace, err := resolver.Resolve(cwd)
		if err != nil {
			return err
		}

		localResources, err := synclocal.Compile(os.DirFS(workspace.Root))
		if err != nil {
			return err
		}

		plans, err := syncapi.NewClient(client).Plan(
			accessToken,
			baseURI,
			workspace.Source,
			dryRun,
			localResources,
		)
		if err != nil {
			return output.NewCmdOutputError(err, cliflags.GetOutputKind(cmd))
		}

		outputKind := cliflags.GetOutputKind(cmd)
		if dryRun {
			return writePlanOutput(cmd.OutOrStdout(), outputKind, plans)
		}
		if len(plans) == 0 {
			return writeSyncOutput(cmd.OutOrStdout(), outputKind, nil, nil)
		}

		confirmationOutput := cmd.ErrOrStderr()
		if err := writePlanReview(
			confirmationOutput,
			"plaintext",
			plans,
			terminalWidth(confirmationOutput),
		); err != nil {
			return err
		}
		if err := validatePlansForApply(plans); err != nil {
			return err
		}
		if !yes {
			confirmed, err := confirmApply(
				cmd.InOrStdin(),
				confirmationOutput,
				terminalStreams,
			)
			if err != nil {
				return err
			}
			if !confirmed {
				_, _ = fmt.Fprintln(confirmationOutput, "Apply canceled.")
				return nil
			}
		}

		syncClient := syncapi.NewClient(client)
		applies := make([]syncapi.ProjectApply, 0, len(plans))
		for _, plan := range plans {
			result, err := syncClient.Apply(
				accessToken,
				baseURI,
				plan.ProjectKey,
				plan.PlanID,
				nil,
			)
			if err != nil {
				_ = writeCompletedSyncOutput(
					cmd.OutOrStdout(),
					outputKind,
					plans,
					applies,
				)
				return output.NewCmdOutputError(err, outputKind)
			}
			applies = append(applies, result)
		}

		return writeCompletedSyncOutput(
			cmd.OutOrStdout(),
			outputKind,
			plans,
			applies,
		)
	}
}

func applyProjectKey(
	cmd *cobra.Command,
	store synclocal.Store,
	storeExists bool,
) (string, error) {
	if cmd.Flags().Changed(cliflags.ProjectFlag) {
		projectKey, _ := cmd.Flags().GetString(cliflags.ProjectFlag)
		if projectKey == "" {
			return "", fmt.Errorf("--project requires a project key")
		}
		return projectKey, nil
	}
	if !storeExists {
		return "", fmt.Errorf(
			"--project is required when applying without a .launchdarkly workspace",
		)
	}

	projectKeys, err := store.ProjectKeys()
	if err != nil {
		return "", err
	}
	switch len(projectKeys) {
	case 1:
		return projectKeys[0], nil
	case 0:
		return "", fmt.Errorf(
			"--project is required because the .launchdarkly workspace has no projects",
		)
	default:
		return "", fmt.Errorf(
			"--project is required because the .launchdarkly workspace has multiple projects",
		)
	}
}

func validatePlansForApply(plans []syncapi.ProjectPlan) error {
	for _, plan := range plans {
		for _, resource := range plan.Resources {
			switch {
			case resource.Error != nil:
				return fmt.Errorf(
					"cannot apply %s/%s: %s",
					plan.ProjectKey,
					resource.LookupKey,
					resource.Error.Message,
				)
			case resource.Status == syncapi.ResourceStatusConflict,
				resource.Status == syncapi.ResourceStatusServerChanged:
				return fmt.Errorf(
					"cannot apply %s/%s with status %s; conflict selection is not supported yet",
					plan.ProjectKey,
					resource.LookupKey,
					resource.Status,
				)
			case resource.SyncDirection == syncapi.SyncDirectionServerCanonical:
				return fmt.Errorf(
					"cannot apply server-canonical resource %s/%s",
					plan.ProjectKey,
					resource.LookupKey,
				)
			}
		}
	}
	return nil
}

type terminalCheck func(io.Reader, io.Writer) bool

func confirmApply(
	input io.Reader,
	prompt io.Writer,
	isTerminal terminalCheck,
) (bool, error) {
	if !isTerminal(input, prompt) {
		return false, fmt.Errorf(
			"interactive apply confirmation requires a terminal; rerun with --yes to apply non-interactively",
		)
	}
	if _, err := fmt.Fprint(prompt, "\nApply these plans? [y/N] "); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read apply confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func terminalStreams(input io.Reader, output io.Writer) bool {
	in, inputIsFile := input.(*os.File)
	out, outputIsFile := output.(*os.File)
	return inputIsFile &&
		outputIsFile &&
		term.IsTerminal(int(in.Fd())) &&
		term.IsTerminal(int(out.Fd()))
}

func writeCompletedSyncOutput(
	out io.Writer,
	outputKind string,
	plans []syncapi.ProjectPlan,
	applies []syncapi.ProjectApply,
) error {
	if outputKind == "json" {
		return writeSyncOutput(out, outputKind, plans, applies)
	}
	return writeSyncOutput(out, outputKind, nil, applies)
}

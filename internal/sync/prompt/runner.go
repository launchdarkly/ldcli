package prompt

import (
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

// Options contains command input and streams for one prompt synchronization.
type Options struct {
	WorkingDirectory string
	AccessToken      string
	BaseURI          string
	OutputKind       string
	ProjectKey       string
	ProjectSpecified bool
	PlanID           string
	Add              bool
	DryRun           bool
	Yes              bool
	Input            io.Reader
	Output           io.Writer
	ErrorOutput      io.Writer
}

type bootstrapRunner func(syncbootstrap.Options) error

// Runner coordinates prompt synchronization using the CLI resource client.
type Runner struct {
	client     resources.Client
	bootstrap  bootstrapRunner
	isTerminal terminalCheck
}

// NewRunner creates a prompt synchronization runner.
func NewRunner(client resources.Client) Runner {
	return Runner{
		client:     client,
		bootstrap:  syncbootstrap.Run,
		isTerminal: terminalStreams,
	}
}

// Run resolves the Git workspace and performs the requested prompt sync flow.
func (runner Runner) Run(options Options) error {
	workspace, err := syncsource.NewResolver().Resolve(options.WorkingDirectory)
	if err != nil {
		return err
	}

	store := synclocal.NewStore(workspace.Root)
	storeExists, err := store.Exists()
	if err != nil {
		return err
	}

	if options.PlanID != "" {
		if options.DryRun || options.Add {
			return fmt.Errorf("--apply cannot be used with --dry-run or --add")
		}
		if _, err := uuid.Parse(options.PlanID); err != nil {
			return fmt.Errorf("invalid plan ID %q: %w", options.PlanID, err)
		}

		projectKey, err := applyProjectKey(options.ProjectKey, options.ProjectSpecified, store, storeExists)
		if err != nil {
			return err
		}
		result, err := syncapi.NewClient(runner.client).Apply(options.AccessToken, options.BaseURI, projectKey, options.PlanID)
		if err != nil {
			return err
		}
		return writeSyncOutput(options.Output, options.OutputKind, nil, nil, []syncapi.ProjectApply{result})
	}

	if !storeExists || options.Add {
		return runner.bootstrap(syncbootstrap.Options{
			Catalog: syncapi.NewCatalogClient(runner.client, options.AccessToken, options.BaseURI),
			Store:   store,
			Input:   options.Input,
			Output:  options.Output,
			Initial: !storeExists,
			DryRun:  options.DryRun,
		})
	}

	return runner.runWorkspaceSync(options, workspace, store)
}

func (runner Runner) runWorkspaceSync(
	options Options,
	workspace syncsource.Workspace,
	store synclocal.Store,
) error {
	projectKeys, err := store.ProjectKeys()
	if err != nil {
		return err
	}
	localResources, err := synclocal.Compile(os.DirFS(workspace.Root))
	if err != nil {
		return err
	}

	outputKind := options.OutputKind
	syncClient := syncapi.NewClient(runner.client)

	// Preview before changing either side so confirmation covers the exact
	// resources and diffs the user reviewed.
	reviewedPlans, err := syncClient.Plan(
		options.AccessToken,
		options.BaseURI,
		workspace.Source,
		true,
		projectKeys,
		localResources,
	)
	if err != nil {
		return err
	}
	if err := planResourceErrors(reviewedPlans); err != nil {
		return err
	}
	if options.DryRun {
		return writePlanOutput(options.Output, outputKind, reviewedPlans)
	}
	if len(reviewedPlans) == 0 {
		return writeSyncOutput(options.Output, outputKind, nil, nil, nil)
	}

	shouldContinue, err := runner.reviewPlans(options, reviewedPlans)
	if err != nil || !shouldContinue {
		return err
	}

	pulled, err := pullServerVariations(
		syncapi.NewCatalogClient(runner.client, options.AccessToken, options.BaseURI),
		store,
		reviewedPlans,
	)
	if err != nil {
		return err
	}

	// Pulling from LaunchDarkly changes local files. Compile that resulting
	// state before creating the durable plan that will actually be applied.
	localResources, err = compileExistingStore(store, workspace.Root)
	if err != nil {
		_ = writeSyncOutput(options.Output, outputKind, pulled, nil, nil)
		return err
	}

	durablePlans, err := syncClient.Plan(
		options.AccessToken,
		options.BaseURI,
		workspace.Source,
		false,
		projectKeys,
		localResources,
	)
	if err != nil {
		_ = writeSyncOutput(options.Output, outputKind, pulled, nil, nil)
		return err
	}
	if err := validateReplannedPlans(reviewedPlans, durablePlans, pulled); err != nil {
		_ = writeSyncOutput(options.Output, outputKind, pulled, durablePlans, nil)
		return err
	}

	applies, err := applyPlans(
		syncClient,
		options.AccessToken,
		options.BaseURI,
		durablePlans,
	)
	if err != nil {
		_ = writeCompletedSyncOutput(
			options.Output,
			outputKind,
			pulled,
			durablePlans,
			applies,
		)
		return err
	}
	if err := store.RemoveEmptyDirectories(); err != nil {
		return err
	}

	return writeCompletedSyncOutput(
		options.Output,
		outputKind,
		pulled,
		durablePlans,
		applies,
	)
}

func compileExistingStore(
	store synclocal.Store,
	workspaceRoot string,
) ([]syncdomain.SyncedResource, error) {
	exists, err := store.Exists()
	if err != nil || !exists {
		return nil, err
	}
	return synclocal.Compile(os.DirFS(workspaceRoot))
}

func applyPlans(
	client syncapi.Client,
	accessToken string,
	baseURI string,
	plans []syncapi.ProjectPlan,
) ([]syncapi.ProjectApply, error) {
	applies := make([]syncapi.ProjectApply, 0, len(plans))
	for _, plan := range plans {
		result, err := client.Apply(
			accessToken,
			baseURI,
			plan.ProjectKey,
			plan.PlanID,
		)
		if err != nil {
			return applies, err
		}
		applies = append(applies, result)
	}
	return applies, nil
}

func applyProjectKey(
	projectKey string,
	projectSpecified bool,
	store synclocal.Store,
	storeExists bool,
) (string, error) {
	if projectSpecified {
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

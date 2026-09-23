package prompt

import (
	"io"
	"os"

	"github.com/launchdarkly/ldcli/internal/resources"
	syncapi "github.com/launchdarkly/ldcli/internal/sync/api"
	syncbootstrap "github.com/launchdarkly/ldcli/internal/sync/bootstrap"
	synclocal "github.com/launchdarkly/ldcli/internal/sync/local"
	syncsource "github.com/launchdarkly/ldcli/internal/sync/source"
)

// Options contains command input and output for one prompt synchronization.
type Options struct {
	WorkingDirectory string
	AccessToken      string
	BaseURI          string
	OutputKind       string
	Add              bool
	DryRun           bool
	Input            io.Reader
	Output           io.Writer
}

type bootstrapRunner func(syncbootstrap.Options) error

// Runner coordinates prompt synchronization using the CLI resource client.
type Runner struct {
	client    resources.Client
	bootstrap bootstrapRunner
}

// NewRunner creates a prompt synchronization runner.
func NewRunner(client resources.Client) Runner {
	return Runner{client: client, bootstrap: syncbootstrap.Run}
}

// Run resolves local prompt resources and asks LaunchDarkly to plan their synchronization.
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

	localResources, err := synclocal.Compile(os.DirFS(workspace.Root))
	if err != nil {
		return err
	}
	plans, err := syncapi.NewClient(runner.client).Plan(
		options.AccessToken,
		options.BaseURI,
		workspace.Source,
		options.DryRun,
		localResources,
	)
	if err != nil {
		return err
	}
	return writePlanOutput(options.Output, options.OutputKind, plans)
}

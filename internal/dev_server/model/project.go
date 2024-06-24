package model

import (
	"context"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
)

type Project struct {
	Key                  string
	SourceEnvironmentKey string
	Context              ldcontext.Context
	LastSyncTime         time.Time
	FlagState            flagstate.AllFlags
}

// CreateProject creates a project and adds it to the database.
func CreateProject(ctx context.Context, projectKey, sourceEnvironmentKey string, ldCtx *ldcontext.Context) (Project, error) {
	project := Project{}
	project.Key = projectKey
	project.SourceEnvironmentKey = sourceEnvironmentKey
	if ldCtx == nil {
		project.Context = ldcontext.NewBuilder("user").Key("dev-environment").Build()
	} else {
		project.Context = *ldCtx
	}

	sdkAdapter := adapters.GetSdk(ctx)
	apiAdapter := adapters.GetApi(ctx)
	store := StoreFromContext(ctx)
	sdkKey, err := apiAdapter.GetSdkKey(ctx, projectKey, sourceEnvironmentKey)
	if err != nil {
		return Project{}, err
	}
	project.FlagState, err = sdkAdapter.GetAllFlagsState(ctx, project.Context, sdkKey)
	if err != nil {
		return Project{}, err
	}
	project.LastSyncTime = time.Now()

	err = store.InsertProject(ctx, project)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

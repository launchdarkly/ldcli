package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
)

func TestPostCopyProject(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	store := mocks.NewMockStore(ctrl)

	ctx := context.Background()
	ctx = model.ContextWithStore(ctx, store)

	s := server{}

	sourceProject := &model.Project{
		Key:                  "source-project",
		SourceEnvironmentKey: "production",
		Context:              ldcontext.NewBuilder("user").Key("test-user").Build(),
		AllFlagsState: model.FlagsState{
			"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 1},
		},
	}

	t.Run("copies project successfully", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), "source-project").Return(sourceProject, nil).Times(2)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(nil)

		request := PostCopyProjectRequestObject{
			ProjectKey: "source-project",
			Body: &PostCopyProjectJSONRequestBody{
				NewProjectKey: "new-project",
			},
		}

		response, err := s.PostCopyProject(ctx, request)
		assert.NoError(t, err)

		successResponse, ok := response.(PostCopyProject201JSONResponse)
		assert.True(t, ok)
		assert.Equal(t, sourceProject.SourceEnvironmentKey, successResponse.SourceEnvironmentKey)
		assert.Equal(t, sourceProject.Context, successResponse.Context)
		assert.NotNil(t, successResponse.FlagsState)
	})

	t.Run("returns 400 when newProjectKey is missing", func(t *testing.T) {
		request := PostCopyProjectRequestObject{
			ProjectKey: "source-project",
			Body: &PostCopyProjectJSONRequestBody{
				NewProjectKey: "",
			},
		}

		response, err := s.PostCopyProject(ctx, request)
		assert.NoError(t, err)

		errorResponse, ok := response.(PostCopyProject400JSONResponse)
		assert.True(t, ok)
		assert.Equal(t, "invalid_request", errorResponse.Code)
		assert.Equal(t, "newProjectKey is required", errorResponse.Message)
	})

	t.Run("returns 404 when source project not found", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), "non-existent").Return(nil, model.NewErrNotFound("project", "non-existent"))

		request := PostCopyProjectRequestObject{
			ProjectKey: "non-existent",
			Body: &PostCopyProjectJSONRequestBody{
				NewProjectKey: "new-project",
			},
		}

		response, err := s.PostCopyProject(ctx, request)
		assert.NoError(t, err)

		errorResponse, ok := response.(PostCopyProject404JSONResponse)
		assert.True(t, ok)
		assert.Equal(t, "not_found", errorResponse.Code)
		assert.Equal(t, "source project not found", errorResponse.Message)
	})

	t.Run("returns 409 when new project already exists", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), "source-project").Return(sourceProject, nil).Times(2)
		store.EXPECT().InsertProject(gomock.Any(), gomock.Any()).Return(model.NewErrAlreadyExists("project", "existing-project"))

		request := PostCopyProjectRequestObject{
			ProjectKey: "source-project",
			Body: &PostCopyProjectJSONRequestBody{
				NewProjectKey: "existing-project",
			},
		}

		response, err := s.PostCopyProject(ctx, request)
		assert.NoError(t, err)

		errorResponse, ok := response.(PostCopyProject409JSONResponse)
		assert.True(t, ok)
		assert.Equal(t, "conflict", errorResponse.Code)
	})
}

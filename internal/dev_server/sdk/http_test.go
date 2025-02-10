package sdk

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

var exampleProjectKey = "my-project"
var exampleProject = &model.Project{
	Key:                  exampleProjectKey,
	SourceEnvironmentKey: "my-environment",
	Context:              ldcontext.Context{},
	LastSyncTime:         time.Unix(0, 0),
	AllFlagsState:        make(model.FlagsState),
	AvailableVariations:  nil,
}

func TestMobileAuth(t *testing.T) {
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)
	observers := model.NewObservers()

	// Wire up sdk routes in test server
	router := mux.NewRouter()
	router.Use(model.ObserversMiddleware(observers))
	router.Use(model.StoreMiddleware(store))
	BindRoutes(router)

	t.Run("given project key prefixed with api_key, it should authenticate successfully", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).Return(exampleProject, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), exampleProjectKey).Return(nil, nil)

		req := httptest.NewRequest("GET", "/msdk/evalx/eyJrZXkiOiJib2FyZCBjYXQifQ==", nil)
		req.Header.Set("Authorization", fmt.Sprintf("api_key %s", exampleProjectKey))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("given just the project key, it should authenticate successfully", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).Return(exampleProject, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), exampleProjectKey).Return(nil, nil)

		req := httptest.NewRequest("GET", "/msdk/evalx/eyJrZXkiOiJib2FyZCBjYXQifQ==", nil)
		req.Header.Set("Authorization", exampleProjectKey)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

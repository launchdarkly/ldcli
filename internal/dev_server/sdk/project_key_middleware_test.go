package sdk

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestGetProjectKeyFromClientSideId(t *testing.T) {
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)

	router := mux.NewRouter()
	router.Use(model.StoreMiddleware(store))

	// Create a test handler that will be wrapped by the middleware
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectKey := GetProjectKeyFromContext(r.Context())
		fmt.Fprint(w, projectKey)
	})

	// Test case: project is found
	t.Run("when project is found for client-side ID", func(t *testing.T) {
		clientSideId := "test-client-side-id"
		expectedProjectKey := "my-project"

		// Set up the mock to return a project
		store.EXPECT().GetDevProjectByClientSideId(gomock.Any(), clientSideId).Return(&model.Project{
			Key: expectedProjectKey,
		}, nil)

		req := httptest.NewRequest("GET", fmt.Sprintf("/sdk/evalx/%s/some/other/path", clientSideId), nil)
		rec := httptest.NewRecorder()

		// Create a subrouter with the middleware
		subrouter := router.PathPrefix("/sdk/evalx/{envId}").Subrouter()
		subrouter.Use(GetProjectKeyFromClientSideId("envId"))
		subrouter.PathPrefix("/").Handler(testHandler)

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, expectedProjectKey, rec.Body.String())
	})

	// Test case: project is not found
	t.Run("when project is not found for client-side ID", func(t *testing.T) {
		clientSideId := "not-found-id"

		// Set up the mock to return a not found error
		store.EXPECT().GetDevProjectByClientSideId(gomock.Any(), clientSideId).Return(nil, model.NewErrNotFound("project", clientSideId))

		req := httptest.NewRequest("GET", fmt.Sprintf("/sdk/evalx/%s/some/other/path", clientSideId), nil)
		rec := httptest.NewRecorder()

		// Create a subrouter with the middleware
		subrouter := router.PathPrefix("/sdk/evalx/{envId}").Subrouter()
		subrouter.Use(GetProjectKeyFromClientSideId("envId"))
		subrouter.PathPrefix("/").Handler(testHandler)

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestParseBasisVersion(t *testing.T) {
	tests := []struct {
		basis    string
		expected int
	}{
		{"", 0},
		{"(p:my-project:5)", 5},
		{"(p:my-project:1)", 1},
		{"(p:complex:key:with:colons:99)", 99},
		{"not-valid", 0},
		{"(p:no-version)", 0},
		{"(p:negative:-1)", 0},
		{"(p:nan:abc)", 0},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("basis=%q", tt.basis), func(t *testing.T) {
			assert.Equal(t, tt.expected, parseBasisVersion(tt.basis))
		})
	}
}

func TestBuildPollResponse(t *testing.T) {
	payloadID := "test-project"
	currentVersion := 5
	flags := model.FlagsState{
		"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 2},
	}

	t.Run("no basis sends xfer-full with payload-missing", func(t *testing.T) {
		resp, err := buildPollResponse(payloadID, currentVersion, flags, 0)
		require.NoError(t, err)

		require.GreaterOrEqual(t, len(resp.Events), 3) // server-intent + put-objects + payload-transferred

		assertServerIntentEvent(t, resp.Events[0], payloadID, currentVersion, subsystems.IntentTransferFull, fdv2ReasonPayloadMissing)
		assertPayloadTransferredEvent(t, resp.Events[len(resp.Events)-1], payloadID, currentVersion)
	})

	t.Run("up-to-date basis sends none with up-to-date", func(t *testing.T) {
		resp, err := buildPollResponse(payloadID, currentVersion, flags, currentVersion)
		require.NoError(t, err)

		require.Len(t, resp.Events, 1)
		assertServerIntentEvent(t, resp.Events[0], payloadID, currentVersion, subsystems.IntentNone, fdv2ReasonUpToDate)
	})

	t.Run("basis ahead of current version sends none with up-to-date", func(t *testing.T) {
		resp, err := buildPollResponse(payloadID, currentVersion, flags, currentVersion+10)
		require.NoError(t, err)

		require.Len(t, resp.Events, 1)
		assertServerIntentEvent(t, resp.Events[0], payloadID, currentVersion, subsystems.IntentNone, fdv2ReasonUpToDate)
	})

	t.Run("stale basis sends xfer-full with cant-catchup", func(t *testing.T) {
		resp, err := buildPollResponse(payloadID, currentVersion, flags, currentVersion-1)
		require.NoError(t, err)

		require.GreaterOrEqual(t, len(resp.Events), 3)
		assertServerIntentEvent(t, resp.Events[0], payloadID, currentVersion, subsystems.IntentTransferFull, fdv2ReasonCantCatchup)
		assertPayloadTransferredEvent(t, resp.Events[len(resp.Events)-1], payloadID, currentVersion)
	})

	t.Run("full transfer includes a put-object for each flag", func(t *testing.T) {
		multiFlags := model.FlagsState{
			"flag-a": model.FlagState{Value: ldvalue.Bool(true), Version: 1},
			"flag-b": model.FlagState{Value: ldvalue.String("hello"), Version: 2},
		}
		resp, err := buildPollResponse(payloadID, currentVersion, multiFlags, 0)
		require.NoError(t, err)

		// server-intent + 2 put-objects + payload-transferred
		assert.Len(t, resp.Events, 4)
		putKeys := make(map[string]bool)
		for _, event := range resp.Events {
			if event.Name == subsystems.EventPutObject {
				var put subsystems.PutObject
				require.NoError(t, json.Unmarshal(event.Data, &put))
				putKeys[put.Key] = true
				assert.Equal(t, currentVersion, put.Version)
				assert.Equal(t, subsystems.FlagKind, put.Kind)
			}
		}
		assert.True(t, putKeys["flag-a"])
		assert.True(t, putKeys["flag-b"])
	})
}

func TestPollV2Handler(t *testing.T) {
	mockController := gomock.NewController(t)
	store := mocks.NewMockStore(mockController)
	observers := model.NewObservers()

	router := mux.NewRouter()
	router.Use(model.ObserversMiddleware(observers))
	router.Use(model.StoreMiddleware(store))
	BindRoutes(router)

	project := &model.Project{
		Key:                  exampleProjectKey,
		SourceEnvironmentKey: "my-environment",
		Context:              ldcontext.Context{},
		LastSyncTime:         time.Unix(0, 0),
		AllFlagsState: model.FlagsState{
			"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 1},
		},
		AvailableVariations: nil,
		PayloadVersion:      3,
	}

	t.Run("no basis returns full payload", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).Return(project, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), exampleProjectKey).Return(nil, nil)

		req := httptest.NewRequest(http.MethodGet, "/sdk/poll", nil)
		req.Header.Set("Authorization", exampleProjectKey)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var resp subsystems.PollingPayload
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.GreaterOrEqual(t, len(resp.Events), 3)
		assertServerIntentEvent(t, resp.Events[0], exampleProjectKey, 3, subsystems.IntentTransferFull, fdv2ReasonPayloadMissing)
		assertPayloadTransferredEvent(t, resp.Events[len(resp.Events)-1], exampleProjectKey, 3)
	})

	t.Run("up-to-date basis returns none intent", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).Return(project, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), exampleProjectKey).Return(nil, nil)

		basisState := fmt.Sprintf("(p:%s:%d)", exampleProjectKey, project.PayloadVersion)
		req := httptest.NewRequest(http.MethodGet, "/sdk/poll?basis="+basisState, nil)
		req.Header.Set("Authorization", exampleProjectKey)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var resp subsystems.PollingPayload
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Len(t, resp.Events, 1)
		assertServerIntentEvent(t, resp.Events[0], exampleProjectKey, 3, subsystems.IntentNone, fdv2ReasonUpToDate)
	})

	t.Run("stale basis returns full payload with cant-catchup", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).Return(project, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), exampleProjectKey).Return(nil, nil)

		basisState := fmt.Sprintf("(p:%s:%d)", exampleProjectKey, project.PayloadVersion-1)
		req := httptest.NewRequest(http.MethodGet, "/sdk/poll?basis="+basisState, nil)
		req.Header.Set("Authorization", exampleProjectKey)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var resp subsystems.PollingPayload
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.GreaterOrEqual(t, len(resp.Events), 3)
		assertServerIntentEvent(t, resp.Events[0], exampleProjectKey, 3, subsystems.IntentTransferFull, fdv2ReasonCantCatchup)
		assertPayloadTransferredEvent(t, resp.Events[len(resp.Events)-1], exampleProjectKey, 3)
	})

	t.Run("unknown project returns 404", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).Return(nil, model.NewErrNotFound("project", exampleProjectKey))

		req := httptest.NewRequest(http.MethodGet, "/sdk/poll", nil)
		req.Header.Set("Authorization", exampleProjectKey)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// assertServerIntentEvent unmarshals a server-intent event and checks its fields.
func assertServerIntentEvent(t *testing.T, event subsystems.RawEvent, payloadID string, target int, intentCode subsystems.IntentCode, reason string) {
	t.Helper()
	assert.Equal(t, subsystems.EventServerIntent, event.Name)
	var data subsystems.ServerIntent
	require.NoError(t, json.Unmarshal(event.Data, &data))
	assert.Equal(t, payloadID, data.Payload.ID)
	assert.Equal(t, target, data.Payload.Target)
	assert.Equal(t, intentCode, data.Payload.Code)
	assert.Equal(t, reason, data.Payload.Reason)
}

// assertPayloadTransferredEvent unmarshals a payload-transferred event and checks its fields.
func assertPayloadTransferredEvent(t *testing.T, event subsystems.RawEvent, payloadID string, version int) {
	t.Helper()
	assert.Equal(t, subsystems.EventPayloadTransferred, event.Name)
	var data subsystems.Selector
	require.NoError(t, json.Unmarshal(event.Data, &data))
	assert.Equal(t, version, data.Version())
	assert.Equal(t, fmt.Sprintf("(p:%s:%d)", payloadID, version), data.State())
}

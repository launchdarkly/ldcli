package sdk

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldreason"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/model/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// exampleContextJSON is the evaluation context a client-side SDK sends, in the form it
// sends it: a JSON object in a POST or REPORT body, or base64url-encoded in a GET path.
const exampleContextJSON = `{"kind":"user","key":"board cat"}`

func exampleContextBase64() string {
	return base64.RawURLEncoding.EncodeToString([]byte(exampleContextJSON))
}

func TestFdv2ClientObjects(t *testing.T) {
	flagState := model.FlagState{Value: ldvalue.String("treatment"), Version: 7, TrackEvents: true}

	t.Run("uses the hyphenated flag-eval object kind", func(t *testing.T) {
		assert.Equal(t, subsystems.ObjectKind("flag-eval"), fdv2ClientObjects(false).kind)
	})

	t.Run("encodes a pre-evaluated result rather than a flag configuration", func(t *testing.T) {
		object := encodeClientObject(t, fdv2ClientObjects(false), "my-flag", flagState)

		assert.Equal(t, map[string]any{
			"flagVersion": float64(7),
			"value":       "treatment",
			"variation":   float64(0),
			"trackEvents": true,
		}, object)
	})

	t.Run("omits the payload version and the flag key, which live on the envelope", func(t *testing.T) {
		object := encodeClientObject(t, fdv2ClientObjects(false), "my-flag", flagState)

		assert.NotContains(t, object, "version")
		assert.NotContains(t, object, "key")
	})

	t.Run("omits samplingRatio because the dev server never samples", func(t *testing.T) {
		object := encodeClientObject(t, fdv2ClientObjects(false), "my-flag", flagState)

		assert.NotContains(t, object, "samplingRatio")
	})

	t.Run("includes a fallthrough reason only when reasons were requested", func(t *testing.T) {
		assert.NotContains(t, encodeClientObject(t, fdv2ClientObjects(false), "my-flag", flagState), "reason")

		object := encodeClientObject(t, fdv2ClientObjects(true), "my-flag", flagState)
		assert.Equal(t, map[string]any{"kind": string(ldreason.EvalReasonFallthrough)}, object["reason"])
	})
}

func encodeClientObject(t *testing.T, objects fdv2ObjectEncoder, key string, flagState model.FlagState) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(objects.encode(key, flagState))
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, json.Unmarshal(encoded, &object))
	return object
}

func TestDecodeBase64Context(t *testing.T) {
	// The context below encodes to "eyJraW5kIjoidXNlciIsImtleSI6ImE_Yi9jIn0=", which
	// exercises both characters that differ between standard and URL-safe base64.
	plain := `{"kind":"user","key":"a?b/c"}`
	standard := base64.StdEncoding.EncodeToString([]byte(plain))

	tests := map[string]string{
		"url-safe without padding": base64.RawURLEncoding.EncodeToString([]byte(plain)),
		"url-safe with padding":    base64.URLEncoding.EncodeToString([]byte(plain)),
		"standard with padding":    standard,
		"standard without padding": strings.TrimRight(standard, "="),
	}

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			decoded, err := decodeBase64Context(encoded)
			require.NoError(t, err)
			assert.Equal(t, plain, string(decoded))
		})
	}

	t.Run("rejects data that is not base64 at all", func(t *testing.T) {
		_, err := decodeBase64Context("not base64!")
		assert.Error(t, err)
	})
}

func TestPollClientV2Handler(t *testing.T) {
	router, store, _ := newClientFdv2TestRouter(t)
	project := &model.Project{
		Key:            exampleProjectKey,
		AllFlagsState:  model.FlagsState{"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 4}},
		PayloadVersion: 3,
	}

	expectProject := func() {
		store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).Return(project, nil)
		store.EXPECT().GetOverridesForProject(gomock.Any(), exampleProjectKey).Return(nil, nil)
	}

	t.Run("GET with the context in the path returns a full payload of flag-eval objects", func(t *testing.T) {
		expectProject()

		rec := serve(router, request(http.MethodGet, "/sdk/poll/eval/"+exampleContextBase64(), "", withAuthHeader))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var resp subsystems.PollingPayload
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Len(t, resp.Events, 3)
		assertServerIntentEvent(t, resp.Events[0], exampleProjectKey, 3, subsystems.IntentTransferFull, fdv2ReasonPayloadMissing)
		assertPayloadTransferredEvent(t, resp.Events[2], exampleProjectKey, 3)

		var put subsystems.PutObject
		require.NoError(t, json.Unmarshal(resp.Events[1].Data, &put))
		assert.Equal(t, subsystems.ObjectKind("flag-eval"), put.Kind)
		assert.Equal(t, "flag-1", put.Key)
		assert.Equal(t, 3, put.Version)
		assert.JSONEq(t, `{"flagVersion":4,"value":true,"variation":0,"trackEvents":false}`, string(put.Object))
	})

	for _, method := range []string{http.MethodPost, methodReport} {
		t.Run(method+" with the context in the body returns a full payload", func(t *testing.T) {
			expectProject()

			rec := serve(router, request(method, "/sdk/poll/eval", exampleContextJSON, withAuthHeader))

			require.Equal(t, http.StatusOK, rec.Code)
			var resp subsystems.PollingPayload
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.Len(t, resp.Events, 3)
			assertServerIntentEvent(t, resp.Events[0], exampleProjectKey, 3, subsystems.IntentTransferFull, fdv2ReasonPayloadMissing)
		})
	}

	t.Run("an up-to-date basis returns a none intent", func(t *testing.T) {
		expectProject()

		basis := fmt.Sprintf("(p:%s:%d)", exampleProjectKey, project.PayloadVersion)
		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval?basis="+basis, exampleContextJSON, withAuthHeader))

		require.Equal(t, http.StatusOK, rec.Code)
		var resp subsystems.PollingPayload
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Len(t, resp.Events, 1)
		assertServerIntentEvent(t, resp.Events[0], exampleProjectKey, 3, subsystems.IntentNone, fdv2ReasonUpToDate)
	})

	t.Run("withReasons adds a reason to each result", func(t *testing.T) {
		expectProject()

		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval?withReasons=true", exampleContextJSON, withAuthHeader))

		require.Equal(t, http.StatusOK, rec.Code)
		var resp subsystems.PollingPayload
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Len(t, resp.Events, 3)
		var put subsystems.PutObject
		require.NoError(t, json.Unmarshal(resp.Events[1].Data, &put))
		assert.Contains(t, string(put.Object), `"reason":{"kind":"FALLTHROUGH"}`)
	})

	t.Run("an unknown project returns 404", func(t *testing.T) {
		store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).
			Return(nil, model.NewErrNotFound("project", exampleProjectKey))

		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval", exampleContextJSON, withAuthHeader))

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestClientFdv2Credentials(t *testing.T) {
	router, store, _ := newClientFdv2TestRouter(t)

	// A mobile key, a client-side ID and the "default" sentinel are all just project keys
	// to the dev server, so the only thing that varies is where the credential arrives.
	t.Run("accepts the credential on the Authorization header", func(t *testing.T) {
		expectEmptyProject(store, exampleProjectKey)

		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval", exampleContextJSON, withAuthHeader))

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("accepts the api_key prefix some SDKs add to the header", func(t *testing.T) {
		expectEmptyProject(store, exampleProjectKey)

		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval", exampleContextJSON, func(r *http.Request) {
			r.Header.Set("Authorization", "api_key "+exampleProjectKey)
		}))

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("accepts the credential in the auth query parameter", func(t *testing.T) {
		expectEmptyProject(store, exampleProjectKey)

		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval?auth="+exampleProjectKey, exampleContextJSON))

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("prefers the Authorization header over the auth query parameter", func(t *testing.T) {
		expectEmptyProject(store, exampleProjectKey)

		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval?auth=from-query-param", exampleContextJSON, withAuthHeader))

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("rejects a request with no credential at all", func(t *testing.T) {
		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval", exampleContextJSON))

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestClientFdv2ContextValidation(t *testing.T) {
	router, _, _ := newClientFdv2TestRouter(t)

	t.Run("rejects a path segment that is not base64", func(t *testing.T) {
		rec := serve(router, request(http.MethodGet, "/sdk/poll/eval/not-valid-base64!", "", withAuthHeader))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("rejects base64 that does not decode to a context", func(t *testing.T) {
		encoded := base64.RawURLEncoding.EncodeToString([]byte("not json"))
		rec := serve(router, request(http.MethodGet, "/sdk/poll/eval/"+encoded, "", withAuthHeader))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("rejects a context that is well-formed JSON but not a valid context", func(t *testing.T) {
		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval", `{"kind":"user"}`, withAuthHeader))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("rejects an empty body", func(t *testing.T) {
		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval", "", withAuthHeader))

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestClientFdv2ContextsAreAccepted(t *testing.T) {
	router, store, _ := newClientFdv2TestRouter(t)

	// The dev server ignores the context, but it must not reject any context an SDK can
	// legitimately build, because doing so would leave the SDK with no data at all.
	contexts := map[string]ldcontext.Context{
		"single kind":     ldcontext.New("user-key"),
		"non-user kind":   ldcontext.NewWithKind("organization", "org-key"),
		"multi-kind":      ldcontext.NewMulti(ldcontext.New("user-key"), ldcontext.NewWithKind("device", "device-key")),
		"with attributes": ldcontext.NewBuilder("user-key").Name("Board Cat").SetBool("beta", true).Build(),
		"anonymous":       ldcontext.NewBuilder("anon-key").Anonymous(true).Build(),
		"with private attr": ldcontext.NewBuilder("user-key").SetString("email", "cat@example.com").
			Private("email").Build(),
	}

	for name, ldContext := range contexts {
		t.Run(name, func(t *testing.T) {
			serialized, err := json.Marshal(ldContext)
			require.NoError(t, err)

			expectEmptyProject(store, exampleProjectKey)
			rec := serve(router, request(http.MethodPost, "/sdk/poll/eval", string(serialized), withAuthHeader))
			assert.Equal(t, http.StatusOK, rec.Code, "context should be accepted in a POST body")

			expectEmptyProject(store, exampleProjectKey)
			encoded := base64.RawURLEncoding.EncodeToString(serialized)
			rec = serve(router, request(http.MethodGet, "/sdk/poll/eval/"+encoded, "", withAuthHeader))
			assert.Equal(t, http.StatusOK, rec.Code, "context should be accepted in a GET path")
		})
	}
}

func TestClientFdv2Cors(t *testing.T) {
	router, _, _ := newClientFdv2TestRouter(t)

	routes := map[string]string{
		"/sdk/poll/eval": http.MethodPost,
		"/sdk/poll/eval/" + exampleContextBase64():   http.MethodGet,
		"/sdk/stream/eval":                           http.MethodPost,
		"/sdk/stream/eval/" + exampleContextBase64(): http.MethodGet,
	}

	for path, method := range routes {
		t.Run("preflights "+method+" "+path, func(t *testing.T) {
			// A preflight carries no credential, so it must be answered before the
			// credential middleware gets a chance to reject it.
			rec := serve(router, request(http.MethodOptions, path, "", func(r *http.Request) {
				r.Header.Set("Origin", "http://localhost:3000")
				r.Header.Set("Access-Control-Request-Method", method)
				r.Header.Set("Access-Control-Request-Headers", "Content-Type")
			}))

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
		})

		t.Run("preflights REPORT "+path, func(t *testing.T) {
			rec := serve(router, request(http.MethodOptions, path, "", func(r *http.Request) {
				r.Header.Set("Origin", "http://localhost:3000")
				r.Header.Set("Access-Control-Request-Method", methodReport)
			}))

			// REPORT is not a CORS-safelisted method, so it must be named explicitly.
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, methodReport, rec.Header().Get("Access-Control-Allow-Methods"))
		})
	}

	t.Run("sends the allow-origin header on an actual request", func(t *testing.T) {
		router, store, _ := newClientFdv2TestRouter(t)
		expectEmptyProject(store, exampleProjectKey)

		rec := serve(router, request(http.MethodPost, "/sdk/poll/eval", exampleContextJSON, withAuthHeader, func(r *http.Request) {
			r.Header.Set("Origin", "http://localhost:3000")
		}))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	})
}

func TestStreamClientV2Handler(t *testing.T) {
	router, store, observers := newClientFdv2TestRouter(t)
	project := &model.Project{
		Key:            exampleProjectKey,
		AllFlagsState:  model.FlagsState{"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 4}},
		PayloadVersion: 3,
	}
	store.EXPECT().GetDevProject(gomock.Any(), exampleProjectKey).Return(project, nil)
	store.EXPECT().GetOverridesForProject(gomock.Any(), exampleProjectKey).Return(nil, nil)

	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/sdk/stream/eval/"+exampleContextBase64(), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", exampleProjectKey)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	events := bufio.NewReader(resp.Body)

	t.Run("opens with the initial full transfer", func(t *testing.T) {
		name, data := readSSEEvent(t, events)
		assert.Equal(t, string(subsystems.EventServerIntent), name)
		assert.Contains(t, data, `"intentCode":"xfer-full"`)

		name, data = readSSEEvent(t, events)
		assert.Equal(t, string(subsystems.EventPutObject), name)
		assert.Contains(t, data, `"kind":"flag-eval"`)
		assert.Contains(t, data, `"flagVersion":4`)

		name, _ = readSSEEvent(t, events)
		assert.Equal(t, string(subsystems.EventPayloadTransferred), name)
	})

	t.Run("pushes an override as a changes transfer", func(t *testing.T) {
		observers.Notify(model.OverrideEvent{
			ProjectKey:     exampleProjectKey,
			FlagKey:        "flag-1",
			FlagState:      model.FlagState{Value: ldvalue.Bool(false), Version: 5},
			PayloadVersion: 4,
		})

		name, data := readSSEEvent(t, events)
		assert.Equal(t, string(subsystems.EventServerIntent), name)
		assert.Contains(t, data, `"intentCode":"xfer-changes"`)

		name, data = readSSEEvent(t, events)
		assert.Equal(t, string(subsystems.EventPutObject), name)
		assert.Contains(t, data, `"kind":"flag-eval"`)
		assert.Contains(t, data, `"value":false`)

		name, data = readSSEEvent(t, events)
		assert.Equal(t, string(subsystems.EventPayloadTransferred), name)
		assert.Contains(t, data, fmt.Sprintf(`"(p:%s:4)"`, exampleProjectKey))
	})

	t.Run("ignores events for other projects", func(t *testing.T) {
		observers.Notify(model.OverrideEvent{
			ProjectKey:     "some-other-project",
			FlagKey:        "flag-1",
			FlagState:      model.FlagState{Value: ldvalue.Bool(true), Version: 6},
			PayloadVersion: 5,
		})
		observers.Notify(model.SyncEvent{
			ProjectKey:     exampleProjectKey,
			AllFlagsState:  model.FlagsState{"flag-1": model.FlagState{Value: ldvalue.Bool(true), Version: 7}},
			PayloadVersion: 6,
		})

		// The next event read is the sync, not the other project's override.
		name, data := readSSEEvent(t, events)
		assert.Equal(t, string(subsystems.EventServerIntent), name)
		assert.Contains(t, data, `"target":6`)
	})
}

// readSSEEvent reads one "event:<name>\ndata:<data>\n\n" frame written by fdv2SSEPayload.
func readSSEEvent(t *testing.T, reader *bufio.Reader) (name string, data string) {
	t.Helper()
	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimPrefix(line, "event:")
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimPrefix(line, "data:")
		case line == "" && name != "":
			return name, data
		}
	}
}

func newClientFdv2TestRouter(t *testing.T) (*mux.Router, *mocks.MockStore, *model.Observers) {
	t.Helper()
	store := mocks.NewMockStore(gomock.NewController(t))
	observers := model.NewObservers()

	router := mux.NewRouter()
	router.Use(model.ObserversMiddleware(observers))
	router.Use(model.StoreMiddleware(store))
	BindRoutes(router)

	return router, store, observers
}

// expectEmptyProject sets up the store to serve a project with no flags, for tests that
// only care about whether the request was accepted.
func expectEmptyProject(store *mocks.MockStore, projectKey string) {
	store.EXPECT().GetDevProject(gomock.Any(), projectKey).
		Return(&model.Project{Key: projectKey, AllFlagsState: model.FlagsState{}, PayloadVersion: 1}, nil)
	store.EXPECT().GetOverridesForProject(gomock.Any(), projectKey).Return(nil, nil)
}

func request(method, target, body string, options ...func(*http.Request)) *http.Request {
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, reader)
	for _, option := range options {
		option(req)
	}
	return req
}

func withAuthHeader(r *http.Request) {
	r.Header.Set("Authorization", exampleProjectKey)
}

func serve(router *mux.Router, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

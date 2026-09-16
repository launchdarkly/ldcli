package sdk

import (
	"net/http"

	"github.com/gorilla/mux"
)

var DevNull = ConstantResponseHandler(http.StatusAccepted, "")

// methodReport is the non-standard HTTP method client-side SDKs use to send an
// evaluation context in the body of an otherwise cacheable read.
const methodReport = "REPORT"

func BindRoutes(router *mux.Router) {
	// events
	router.HandleFunc("/bulk", SdkEventsReceiveHandler)
	router.HandleFunc("/diagnostic", DevNull)
	router.Methods(http.MethodPost, http.MethodOptions).Path("/events/bulk/{envId}").Handler(EventsCorsHeaders(DevNull))
	router.Methods(http.MethodPost, http.MethodOptions).Path("/events/diagnostic/{envId}").Handler(EventsCorsHeaders(DevNull))
	router.HandleFunc("/mobile", DevNull)
	router.HandleFunc("/mobile/events", DevNull)
	router.HandleFunc("/mobile/events/bulk", SdkEventsReceiveHandler)
	router.HandleFunc("/mobile/events/diagnostic", DevNull)

	router.Handle("/all", GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(StreamServerAllPayload)))
	router.Handle("/sdk/latest-all", GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(LatestAll)))
	router.Handle("/sdk/poll", GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(PollV2)))
	router.Handle("/sdk/stream", GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(StreamV2)))

	router.PathPrefix("/sdk/flags/{flagKey}").
		Methods(http.MethodGet).
		Handler(GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(GetServerFlags)))
	router.PathPrefix("/sdk/flags").
		Methods(http.MethodGet).
		Handler(GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(GetServerFlags)))

	router.PathPrefix("/meval").Handler(GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(StreamClientFlags)))
	router.PathPrefix("/msdk/evalx").Handler(GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(GetClientFlags)))

	evalRouter := router.PathPrefix("/eval").Subrouter()
	evalRouter.Use(CorsHeaders)
	evalRouter.Use(GetProjectKeyFromEnvIdParameter("envId"))
	evalRouter.PathPrefix("/{envId}").
		Methods(http.MethodGet, methodReport, http.MethodOptions).
		HandlerFunc(StreamClientFlags)

	goalsRouter := router.Path("/sdk/goals/{envId}").Subrouter()
	goalsRouter.Use(CorsHeaders)
	goalsRouter.Use(GetProjectKeyFromEnvIdParameter("envId"))
	goalsRouter.Methods(http.MethodGet, http.MethodOptions).HandlerFunc(ConstantResponseHandler(http.StatusOK, "[]"))

	evalXRouter := router.PathPrefix("/sdk/evalx/{envId}").Subrouter()
	evalXRouter.Use(CorsHeaders)
	evalXRouter.Use(GetProjectKeyFromEnvIdParameter("envId"))
	evalXRouter.Methods(http.MethodGet, http.MethodOptions, methodReport).HandlerFunc(GetClientFlags)

	// FDv2 unifies the browser and mobile client-side endpoints, so these four routes
	// replace the /eval/{envId}, /meval, /sdk/evalx/{envId} and /msdk/evalx families above.
	bindClientFdv2Route(router, "/sdk/poll/eval", PollClientV2, http.MethodPost, methodReport)
	bindClientFdv2Route(router, "/sdk/poll/eval/{context}", PollClientV2, http.MethodGet)
	bindClientFdv2Route(router, "/sdk/stream/eval", StreamClientV2, http.MethodPost, methodReport)
	bindClientFdv2Route(router, "/sdk/stream/eval/{context}", StreamClientV2, http.MethodGet)
}

// bindClientFdv2Route registers a client-side FDv2 route along with the three pieces of
// middleware the protocol requires of every one of them: CORS (including the OPTIONS
// preflight, which the gorilla handler answers before the rest of the chain runs),
// credential resolution, and evaluation context validation.
func bindClientFdv2Route(router *mux.Router, path string, handler http.HandlerFunc, methods ...string) {
	route := router.Path(path).Subrouter()
	route.Use(ClientFdv2CorsHeaders)
	route.Use(GetProjectKeyFromClientCredential)
	route.Use(ParseClientContext)
	route.Methods(append(methods, http.MethodOptions)...).HandlerFunc(handler)
}

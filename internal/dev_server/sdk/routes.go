package sdk

import (
	"net/http"

	"github.com/gorilla/mux"
)

var DevNull = ConstantResponseHandler(http.StatusAccepted, "")

func BindRoutes(router *mux.Router) {
	// events
	router.HandleFunc("/bulk", DevNull)
	router.HandleFunc("/diagnostic", DevNull)
	router.Methods(http.MethodPost, http.MethodOptions).Path("/events/bulk/{envId}").Handler(EventsCorsHeaders(DevNull))
	router.Methods(http.MethodPost, http.MethodOptions).Path("/events/diagnostic/{envId}").Handler(EventsCorsHeaders(DevNull))
	router.HandleFunc("/mobile", DevNull)
	router.HandleFunc("/mobile/events", DevNull)
	router.HandleFunc("/mobile/events/bulk", DevNull)
	router.HandleFunc("/mobile/events/diagnostic", DevNull)

	router.Handle("/all", GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(StreamServerAllPayload)))
	router.Handle("/sdk/latest-all", GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(LatestAll)))

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
		Methods(http.MethodGet, "REPORT", http.MethodOptions).
		HandlerFunc(StreamClientFlags)

	goalsRouter := router.Path("/sdk/goals/{envId}").Subrouter()
	goalsRouter.Use(CorsHeaders)
	goalsRouter.Use(GetProjectKeyFromEnvIdParameter("envId"))
	goalsRouter.Methods(http.MethodGet, http.MethodOptions).HandlerFunc(ConstantResponseHandler(http.StatusOK, "[]"))

	evalXRouter := router.PathPrefix("/sdk/evalx/{envId}").Subrouter()
	evalXRouter.Use(CorsHeaders)
	evalXRouter.Use(GetProjectKeyFromEnvIdParameter("envId"))
	evalXRouter.Methods(http.MethodGet, http.MethodOptions, "REPORT").HandlerFunc(GetClientFlags)
}

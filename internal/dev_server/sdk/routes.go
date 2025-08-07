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
	router.Handle("/events/bulk/{envId}", EventsCorsHeaders(DevNull))
	router.Handle("/events/diagnostic/{envId}", EventsCorsHeaders(DevNull))
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
	evalRouter.Methods(http.MethodOptions).HandlerFunc(ConstantResponseHandler(http.StatusOK, ""))
	evalRouter.Use(GetProjectKeyFromClientSideId("envId"))
	evalRouter.PathPrefix("/{envId}").
		Methods(http.MethodGet, "REPORT").
		HandlerFunc(StreamClientFlags)

	goalsRouter := router.Path("/sdk/goals/{envId}").Subrouter()
	goalsRouter.Use(CorsHeaders)
	goalsRouter.Use(GetProjectKeyFromClientSideId("envId"))
	goalsRouter.Methods(http.MethodOptions).HandlerFunc(ConstantResponseHandler(http.StatusOK, ""))
	goalsRouter.Methods(http.MethodGet).HandlerFunc(ConstantResponseHandler(http.StatusOK, "[]"))

	evalXRouter := router.PathPrefix("/sdk/evalx/{envId}").Subrouter()
	evalXRouter.Use(CorsHeaders)
	evalXRouter.Use(GetProjectKeyFromClientSideId("envId"))
	evalXRouter.Methods(http.MethodOptions).HandlerFunc(ConstantResponseHandler(http.StatusOK, ""))
	evalXRouter.PathPrefix("/").Methods(http.MethodGet, "REPORT").HandlerFunc(GetClientFlags)
}

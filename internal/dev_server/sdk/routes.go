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

	router.PathPrefix("/meval").Handler(GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(StreamClientFlags)))
	router.PathPrefix("/msdk/evalx").Handler(GetProjectKeyFromAuthorizationHeader(http.HandlerFunc(GetClientFlags)))

	evalRouter := router.PathPrefix("/eval").Subrouter()
	evalRouter.Use(CorsHeaders)
	evalRouter.Methods("OPTIONS").HandlerFunc(ConstantResponseHandler(http.StatusOK, ""))
	evalRouter.Use(GetProjectKeyFromEnvIdParameter("envId"))
	evalRouter.HandleFunc("/{envId}", StreamClientFlags)
	evalRouter.HandleFunc("/{envId}/{contextBase64}", StreamClientFlags)

	clientsideSdkRouter := router.PathPrefix("/sdk").Subrouter()
	clientsideSdkRouter.Use(CorsHeaders)
	clientsideSdkRouter.Methods("OPTIONS").HandlerFunc(ConstantResponseHandler(http.StatusOK, ""))
	clientsideSdkRouter.Use(GetProjectKeyFromEnvIdParameter("envId"))
	clientsideSdkRouter.HandleFunc("/goals/{envId}", ConstantResponseHandler(http.StatusOK, "[]"))
	clientsideSdkRouter.PathPrefix("/evalx/{envId}").HandlerFunc(GetClientFlags)

	/*
	 */
}

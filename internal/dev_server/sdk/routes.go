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
	// TODO need cors for events on client side
	router.HandleFunc("/events/bulk/{envId}", DevNull)
	router.HandleFunc("/events/diagnostic/{envId}", DevNull)
	router.HandleFunc("/mobile", DevNull)
	router.HandleFunc("/mobile/events", DevNull)
	router.HandleFunc("/mobile/events/bulk", DevNull)
	router.HandleFunc("/mobile/events/diagnostic", DevNull)

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
	clientsideSdkRouter.HandleFunc("/evalx/{envId}/contexts/{contextBase64}", GetClientFlags)
	clientsideSdkRouter.HandleFunc("/evalx/{envId}/contexts", GetClientFlags)
	clientsideSdkRouter.HandleFunc("/evalx/{envId}/users", GetClientFlags)
	clientsideSdkRouter.HandleFunc("/evalx/{envId}/users/{userBase64}", GetClientFlags)

	/*
			/all	GET	stream.	SSE stream for all data
		✅	/bulk	POST	events.	Receives analytics events from SDKs
		✅	/diagnostic	POST	events.	Receives diagnostic data from SDKs
			/flags	GET	stream.	SSE stream for flag data (older SDKs)
			/sdk/flags	GET	sdk.	Polling endpoint for PHP SDK
			/sdk/flags/{flagKey}	GET	sdk.	Polling endpoint for PHP SDK
			/sdk/segments/{segmentKey}	GET	sdk.	Polling endpoint for PHP SDK
			/meval/{contextBase64}	GET	clientstream.	SSE stream of "ping" and other events
			/meval	REPORT	clientstream.	Same as above, but request body is the evaluation context JSON object (not in base64)
		✅	/mobile	POST	events.	For receiving events from mobile SDKs
		✅	/mobile/events	POST	events.	Same as above
		✅	/mobile/events/bulk	POST	events.	Same as above
		✅	/mobile/events/diagnostic	POST	events.	Same as above
			/mping	GET	clientstream.	SSE stream for older SDKs that issues "ping" events when flags have changed
			/msdk/evalx/contexts/{contextBase64}	GET	clientsdk.	Polling endpoint, returns flag evaluation results for an evaluation context
			/msdk/evalx/context	REPORT	clientsdk.	Same as above but request body is the evaluation context JSON object (not in base64)
			/msdk/evalx/users/{contextBase64}	GET	clientsdk.	Alternate name for /msdk/evalx/contexts/{contextBase64} used by older SDKs
			/msdk/evalx/user	REPORT	clientsdk.	Alternate name for /msdk/evalx/context used by older SDKs
			/a/{envId}.gif?d=*events*	GET	events.	Alternative analytics event mechanism used if browser does not allow CORS
		✅	/eval/{envId}/{contextBase64}	GET	clientstream.	SSE stream of "ping" and other events for JS and other client-side SDK listeners
		✅	/eval/{envId}	REPORT	clientstream.	Same as above but request body is the evaluation context JSON object (not in base64)
		✅	/events/bulk/{envId}	POST	events.	Receives analytics events from SDKs
		✅	/events/diagnostic/{envId}	POST	events.	Receives diagnostic data from SDKs
			/ping/{envId}	GET	clientstream.	SSE stream for older SDKs that issues "ping" events when flags have changed
		✅	/sdk/evalx/{envId}/contexts/{contextBase64}	GET	clientsdk.	Polling endpoint, returns flag evaluation results and additional metadata
		✅	/sdk/evalx/{envId}/contexts	REPORT	clientsdk.	Same as above but request body is the evaluation context JSON object (not in base64)
		✅	/sdk/evalx/{envId}/users/{contextBase64}	GET	clientsdk.	Alternate name for /sdk/evalx/{envId}/contexts/{contextBase64} used by older SDKs
		✅	/sdk/evalx/{envId}/users	REPORT	clientsdk.	Alternate name for /sdk/evalx/{envId}/contexts used by older SDKs
		✅	/sdk/goals/{envId}	GET	clientsdk.	Provides goals data used by JS SDK
	*/
}

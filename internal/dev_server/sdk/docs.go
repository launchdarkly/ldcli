// Package sdk provides API handlers for [the endpoints provided by ld-relay](https://github.com/launchdarkly/ld-relay/blob/v8/docs/endpoints.md).
//
// Not all routes are supported because some routes are for exclusively for SDKs that have reached end of life.
//
//	✅	/all	GET	stream.	SSE stream for all data
//	✅	/bulk	POST	events.	Receives analytics events from SDKs
//	✅	/diagnostic	POST	events.	Receives diagnostic data from SDKs
//	❌ 	/flags	GET	stream.	SSE stream for flag data (older SDKs)
//	✅ 	/sdk/flags	GET	sdk.	Polling endpoint for PHP SDK
//	✅ 	/sdk/flags/{flagKey}	GET	sdk.	Polling endpoint for PHP SDK
//	✅	/sdk/segments/{segmentKey}	GET	sdk.	Polling endpoint for PHP SDK (Segments are not used in dev server, so this always 404s)
//	✅	/meval/{contextBase64}	GET	clientstream.	SSE stream of "ping" and other events
//	✅	/meval	REPORT	clientstream.	Same as above, but request body is the evaluation context JSON object (not in base64)
//	✅	/mobile	POST	events.	For receiving events from mobile SDKs
//	✅	/mobile/events	POST	events.	Same as above
//	✅	/mobile/events/bulk	POST	events.	Same as above
//	✅	/mobile/events/diagnostic	POST	events.	Same as above
//	❌ 	/mping	GET	clientstream.	SSE stream for older SDKs that issues "ping" events when flags have changed
//	✅	/msdk/evalx/contexts/{contextBase64}	GET	clientsdk.	Polling endpoint, returns flag evaluation results for an evaluation context
//	✅	/msdk/evalx/context	REPORT	clientsdk.	Same as above but request body is the evaluation context JSON object (not in base64)
//	✅	/msdk/evalx/users/{contextBase64}	GET	clientsdk.	Alternate name for /msdk/evalx/contexts/{contextBase64} used by older SDKs
//	✅	/msdk/evalx/user	REPORT	clientsdk.	Alternate name for /msdk/evalx/context used by older SDKs
//	❌ 	/a/{envId}.gif?d=*events*	GET	events.	Alternative analytics event mechanism used if browser does not allow CORS
//	✅	/eval/{envId}/{contextBase64}	GET	clientstream.	SSE stream of "ping" and other events for JS and other client-side SDK listeners
//	✅	/eval/{envId}	REPORT	clientstream.	Same as above but request body is the evaluation context JSON object (not in base64)
//	✅	/events/bulk/{envId}	POST	events.	Receives analytics events from SDKs
//	✅	/events/diagnostic/{envId}	POST	events.	Receives diagnostic data from SDKs
//	❌ 	/ping/{envId}	GET	clientstream.	SSE stream for older SDKs that issues "ping" events when flags have changed
//	✅	/sdk/evalx/{envId}/contexts/{contextBase64}	GET	clientsdk.	Polling endpoint, returns flag evaluation results and additional metadata
//	✅	/sdk/evalx/{envId}/contexts	REPORT	clientsdk.	Same as above but request body is the evaluation context JSON object (not in base64)
//	✅	/sdk/evalx/{envId}/users/{contextBase64}	GET	clientsdk.	Alternate name for /sdk/evalx/{envId}/contexts/{contextBase64} used by older SDKs
//	✅	/sdk/evalx/{envId}/users	REPORT	clientsdk.	Alternate name for /sdk/evalx/{envId}/contexts used by older SDKs
//	✅	/sdk/goals/{envId}	GET	clientsdk.	Provides goals data used by JS SDK
//
// The routes above are the FDv1 protocol. FDv2 replaces them with the routes below, which
// carry the same flag data as a stream of protocol events. FDv2 also unifies the browser and
// mobile client-side endpoints, so a single pair of routes replaces the four FDv1 client-side
// route families (/eval/{envId}, /meval, /sdk/evalx/{envId} and /msdk/evalx). Delta transfers
// are not supported: a client whose ?basis is stale receives a full payload.
//
//	✅	/sdk/poll	GET	sdk.	Polling endpoint for server-side SDKs
//	✅	/sdk/stream	GET	stream.	SSE stream for server-side SDKs
//	✅	/sdk/poll/eval/{contextBase64}	GET	clientsdk.	Polling endpoint returning evaluation results for a context
//	✅	/sdk/poll/eval	POST, REPORT	clientsdk.	Same as above, but the request body is the evaluation context JSON object (not in base64)
//	✅	/sdk/stream/eval/{contextBase64}	GET	clientstream.	SSE stream of evaluation results for a context
//	✅	/sdk/stream/eval	POST, REPORT	clientstream.	Same as above, but the request body is the evaluation context JSON object (not in base64)
package sdk

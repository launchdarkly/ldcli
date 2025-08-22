package events

import "github.com/gorilla/mux"

func BindRoutes(router *mux.Router) {
	router.HandleFunc("/events/tee", SdkEventsTeeHandler)
}

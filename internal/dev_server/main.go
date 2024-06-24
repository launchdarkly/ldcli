package main

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/ldcli/internal/dev_server/api"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
)

// FIXME this is gonna get replaced by a cli command
func main() {
	ctx := context.Background()
	sqlStore, err := db.NewSqlite(ctx, "devserver.db")
	if err != nil {
		log.Fatal(err)
	}
	ss := api.NewStrictServer(sqlStore)
	apiServer := api.NewStrictHandler(ss, nil)
	r := mux.NewRouter()
	// TODO need a subrouter for relay endpoints
	// TODO need to wire up sqlite
	handler := api.HandlerFromMux(apiServer, r)
	server := http.Server{
		Addr:    "0.0.0.0:8765",
		Handler: handler,
	}
	log.Fatal(server.ListenAndServe())
}

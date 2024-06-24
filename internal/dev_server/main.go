package main

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
	"github.com/launchdarkly/ldcli/internal/dev_server/api"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

// FIXME this is gonna get replaced by a cli command
func main() {
	ctx := context.Background()
	sqlStore, err := db.NewSqlite(ctx, "devserver.db")
	if err != nil {
		log.Fatal(err)
	}
	ss := api.NewStrictServer()
	apiServer := api.NewStrictHandler(ss, nil)
	r := mux.NewRouter()
	r.Use(adapters.Middleware(ldapi.APIClient{})) // TODO pass in a real client
	r.Use(model.StoreMiddleware(sqlStore))
	// TODO need a subrouter for relay endpoints
	handler := api.HandlerFromMux(apiServer, r)
	server := http.Server{
		Addr:    "0.0.0.0:8765",
		Handler: handler,
	}
	log.Fatal(server.ListenAndServe())
}

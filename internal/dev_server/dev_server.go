package dev_server

import (
	"context"
	ldapi "github.com/launchdarkly/api-client-go/v14"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/ldcli/internal/dev_server/api"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
)

func RunServer() {
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

package dev_server

import (
	"context"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/launchdarkly/ldcli/internal/client"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
	"github.com/launchdarkly/ldcli/internal/dev_server/api"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
)

type Client interface {
	RunServer(ctx context.Context, accessToken, baseURI string)
}

type LDClient struct {
	cliVersion string
}

var _ Client = LDClient{}

func NewClient(cliVersion string) LDClient {
	return LDClient{cliVersion: cliVersion}
}

func (c LDClient) RunServer(ctx context.Context, accessToken, baseURI string) {
	ldClient := client.New(accessToken, baseURI, c.cliVersion)
	sqlStore, err := db.NewSqlite(ctx, "devserver.db")
	if err != nil {
		log.Fatal(err)
	}
	ss := api.NewStrictServer()
	apiServer := api.NewStrictHandler(ss, nil)
	r := mux.NewRouter()
	r.Use(adapters.Middleware(*ldClient))
	r.Use(model.StoreMiddleware(sqlStore))
	// TODO need a subrouter for relay endpoints
	handler := api.HandlerFromMux(apiServer, r)
	server := http.Server{
		Addr:    "0.0.0.0:8765",
		Handler: handler,
	}
	log.Println("Listening on http://0.0.0.0:8765")
	log.Fatal(server.ListenAndServe())
}

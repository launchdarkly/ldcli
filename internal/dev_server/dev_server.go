package dev_server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/launchdarkly/ldcli/internal/dev_server/sdk"

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
	apiServer := api.NewStrictHandlerWithOptions(ss, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  RequestErrorHandler,
		ResponseErrorHandlerFunc: ResponseErrorHandler,
	})
	r := mux.NewRouter()
	r.Use(adapters.Middleware(*ldClient, "https://events.ld.catamorphic.com", "https://relay-stg.ld.catamorphic.com", "https://relay-stg.ld.catamorphic.com")) // TODO add to config
	r.Use(model.StoreMiddleware(sqlStore))
	sdk.BindRoutes(r)
	handler := api.HandlerFromMux(apiServer, r)
	handler = handlers.CombinedLoggingHandler(os.Stdout, handler)
	handler = handlers.RecoveryHandler(handlers.PrintRecoveryStack(true))(handler)
	fmt.Println("Server running on 0.0.0.0:8765")
	server := http.Server{
		Addr:    "0.0.0.0:8765",
		Handler: handler,
	}
	log.Fatal(server.ListenAndServe())
}

func ResponseErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("Error while serving response: %+v", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
func RequestErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("Error while serving request: %+v", err)
	http.Error(w, err.Error(), http.StatusBadRequest)
}

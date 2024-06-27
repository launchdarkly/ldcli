package dev_server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/launchdarkly/ldcli/internal/dev_server/sdk"
	"github.com/mitchellh/go-homedir"

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
	dbPath := getDBPath()
	log.Printf("Using database at %s", dbPath)
	sqlStore, err := db.NewSqlite(ctx, getDBPath())
	if err != nil {
		log.Fatal(err)
	}
	ss := api.NewStrictServer()
	apiServer := api.NewStrictHandlerWithOptions(ss, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  RequestErrorHandler,
		ResponseErrorHandlerFunc: ResponseErrorHandler,
	})
	r := mux.NewRouter()
	r.Use(adapters.Middleware(*ldClient, "https://relay-stg.ld.catamorphic.com")) // TODO add to config
	r.Use(model.StoreMiddleware(sqlStore))
	r.Use(model.ObserversMiddleware(model.NewObservers()))
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

func getDBPath() string {
	statePath := os.Getenv("XDG_STATE_HOME")
	if statePath == "" {
		home, _ := homedir.Dir()
		statePath = filepath.Join(home, ".local/state")
	}
	if err := os.MkdirAll(statePath, 0700 /* only current user can access */); err != nil {
		log.Fatalf("Unable to create state directory: %s", err)
	}
	return filepath.Join(statePath, "dev_server.db")
}

package dev_server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/adrg/xdg"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"github.com/launchdarkly/ldcli/internal/client"
	"github.com/launchdarkly/ldcli/internal/dev_server/adapters"
	"github.com/launchdarkly/ldcli/internal/dev_server/api"
	"github.com/launchdarkly/ldcli/internal/dev_server/db"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/launchdarkly/ldcli/internal/dev_server/sdk"
	"github.com/launchdarkly/ldcli/internal/dev_server/ui"
)

type Client interface {
	RunServer(ctx context.Context, serverParams ServerParams)
}

type ServerParams struct {
	AccessToken            string
	BaseURI                string
	DevStreamURI           string
	Port                   string
	InitialProjectSettings model.InitialProjectSettings
}

type LDClient struct {
	cliVersion string
}

var _ Client = LDClient{}

func NewClient(cliVersion string) LDClient {
	return LDClient{cliVersion: cliVersion}
}

func (c LDClient) RunServer(ctx context.Context, serverParams ServerParams) {
	ldClient := client.New(serverParams.AccessToken, serverParams.BaseURI, c.cliVersion)
	dbPath := getDBPath()
	log.Printf("Using database at %s", dbPath)
	sqlStore, err := db.NewSqlite(ctx, getDBPath())
	if err != nil {
		log.Fatal(err)
	}
	observers := model.NewObservers()
	ss := api.NewStrictServer()
	apiServer := api.NewStrictHandlerWithOptions(ss, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  api.RequestErrorHandler,
		ResponseErrorHandlerFunc: api.ResponseErrorHandler,
	})
	r := mux.NewRouter()
	r.Use(adapters.Middleware(*ldClient, serverParams.DevStreamURI))
	r.Use(model.StoreMiddleware(sqlStore))
	r.Use(model.ObserversMiddleware(observers))
	r.Handle("/", http.RedirectHandler("/ui/", http.StatusFound))
	r.Handle("/ui", http.RedirectHandler("/ui/", http.StatusMovedPermanently))
	r.PathPrefix("/ui/").Handler(http.StripPrefix("/ui/", ui.AssetHandler))
	sdk.BindRoutes(r)
	handler := api.HandlerFromMux(apiServer, r)
	handler = handlers.CombinedLoggingHandler(os.Stdout, handler)
	handler = handlers.RecoveryHandler(handlers.PrintRecoveryStack(true))(handler)

	ctx = adapters.WithLdApi(ctx, *ldClient, serverParams.DevStreamURI)
	ctx = model.SetObserversOnContext(ctx, observers)
	ctx = model.ContextWithStore(ctx, sqlStore)
	syncErr := model.CreateOrSyncProject(ctx, serverParams.InitialProjectSettings)
	if syncErr != nil {
		log.Fatal(syncErr)
	}

	addr := fmt.Sprintf("0.0.0.0:%s", serverParams.Port)
	log.Printf("Server running on %s", addr)
	log.Printf("Access the UI for toggling overrides at http://localhost:%s/ui or by running `ldcli dev-server ui`", serverParams.Port)

	server := http.Server{
		Addr:    addr,
		Handler: handler,
	}
	log.Fatal(server.ListenAndServe())
}

func getDBPath() string {
	dbFilePath, err := xdg.StateFile("ldcli/dev_server.db")
	log.Printf("Using database at %s", dbFilePath)
	if err != nil {
		log.Fatalf("Unable to create state directory: %s", err)
	}
	return dbFilePath
}

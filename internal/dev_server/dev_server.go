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
	CorsEnabled            bool
	CorsOrigin             string
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
	r.Use(handlers.RecoveryHandler(handlers.PrintRecoveryStack(true)))
	r.Use(adapters.Middleware(*ldClient, serverParams.DevStreamURI))
	r.Use(model.StoreMiddleware(sqlStore))
	r.Use(model.ObserversMiddleware(observers))
	r.Handle("/", http.RedirectHandler("/ui/", http.StatusFound))
	r.Handle("/ui", http.RedirectHandler("/ui/", http.StatusMovedPermanently))
	r.PathPrefix("/ui/").Handler(http.StripPrefix("/ui/", ui.AssetHandler))

	sdk.BindRoutes(r)

	apiRouter := r.PathPrefix("/dev").Subrouter()
	if serverParams.CorsEnabled {
		apiRouter.Use(handlers.CORS(
			handlers.AllowedOrigins([]string{serverParams.CorsOrigin}),
			handlers.AllowedHeaders([]string{"Content-Type", "Content-Length", "Accept-Encoding", "X-Requested-With"}),
			handlers.ExposedHeaders([]string{"Date", "Content-Length"}),
			handlers.AllowedMethods([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}),
			handlers.MaxAge(300),
		))
		apiRouter.PathPrefix("/").Methods(http.MethodOptions).HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// request should have been handled in the CORS middleware
			// This route is needed so that gorilla will not 404 for options requests
			panic("options handler running. This indicates a misconfiguration of routes")
		})
	}
	api.HandlerFromMux(apiServer, apiRouter) // this method actually mutates the passed router.

	ctx = adapters.WithApiAndSdk(ctx, *ldClient, serverParams.DevStreamURI)
	ctx = model.SetObserversOnContext(ctx, observers)
	ctx = model.ContextWithStore(ctx, sqlStore)
	syncErr := model.CreateOrSyncProject(ctx, serverParams.InitialProjectSettings)
	if syncErr != nil {
		log.Fatal(syncErr)
	}
	handler := handlers.CombinedLoggingHandler(os.Stdout, r)

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

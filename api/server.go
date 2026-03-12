package api

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/dumps"
	"github.com/danielgormly/devctl/install"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

// Server is the HTTP server that serves the API and embedded frontend.
type Server struct {
	db          *sql.DB
	queries     *dbq.Queries
	registry    *services.Registry
	manager     *services.Manager
	supervisor  *services.Supervisor
	poller      *services.Poller
	dumps       *dumps.Server
	caddy       *sites.CaddyClient
	siteManager *sites.Manager
	installers  map[string]install.Installer
	siteHome    string // home directory of the non-root site user
	devctlAddr  string // listen address passed to EnsureHTTPServer (e.g. "127.0.0.1:4000")
	mux         *http.ServeMux
	uiFS        embed.FS
}

// NewServer creates and configures the HTTP server.
func NewServer(
	db *sql.DB,
	registry *services.Registry,
	manager *services.Manager,
	supervisor *services.Supervisor,
	poller *services.Poller,
	dumpsServer *dumps.Server,
	caddyClient *sites.CaddyClient,
	siteManager *sites.Manager,
	installers map[string]install.Installer,
	uiFS embed.FS,
	siteHome string,
	devctlAddr string,
) *Server {
	s := &Server{
		db:          db,
		queries:     dbq.New(db),
		registry:    registry,
		manager:     manager,
		supervisor:  supervisor,
		poller:      poller,
		dumps:       dumpsServer,
		caddy:       caddyClient,
		siteManager: siteManager,
		installers:  installers,
		siteHome:    siteHome,
		devctlAddr:  devctlAddr,
		mux:         http.NewServeMux(),
		uiFS:        uiFS,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// Services
	s.mux.HandleFunc("GET /api/services", s.handleGetServices)
	s.mux.HandleFunc("POST /api/services/{id}/start", s.handleServiceStart)
	s.mux.HandleFunc("POST /api/services/{id}/stop", s.handleServiceStop)
	s.mux.HandleFunc("POST /api/services/{id}/restart", s.handleServiceRestart)
	s.mux.HandleFunc("POST /api/services/{id}/install", s.handleServiceInstall)
	s.mux.HandleFunc("DELETE /api/services/{id}", s.handleServicePurge)
	s.mux.HandleFunc("GET /api/services/{id}/logs", s.handleServiceLogs)
	s.mux.HandleFunc("GET /api/services/{id}/credentials", s.handleServiceCredentials)
	s.mux.HandleFunc("GET /api/services/events", s.handleServiceEvents)

	// Sites
	s.mux.HandleFunc("GET /api/sites", s.handleGetSites)
	s.mux.HandleFunc("POST /api/sites", s.handleCreateSite)
	s.mux.HandleFunc("GET /api/sites/{id}", s.handleGetSite)
	s.mux.HandleFunc("PUT /api/sites/{id}", s.handleUpdateSite)
	s.mux.HandleFunc("DELETE /api/sites/{id}", s.handleDeleteSite)
	s.mux.HandleFunc("POST /api/sites/{id}/spx/enable", s.handleSPXEnable)
	s.mux.HandleFunc("POST /api/sites/{id}/spx/disable", s.handleSPXDisable)

	// PHP
	s.mux.HandleFunc("GET /api/php/versions", s.handleGetPHPVersions)
	s.mux.HandleFunc("POST /api/php/versions/{ver}/install", s.handleInstallPHP)
	s.mux.HandleFunc("DELETE /api/php/versions/{ver}", s.handleUninstallPHP)
	s.mux.HandleFunc("POST /api/php/versions/{ver}/start", s.handlePHPFPMStart)
	s.mux.HandleFunc("POST /api/php/versions/{ver}/stop", s.handlePHPFPMStop)
	s.mux.HandleFunc("POST /api/php/versions/{ver}/restart", s.handlePHPFPMRestart)
	s.mux.HandleFunc("GET /api/php/settings", s.handleGetPHPSettings)
	s.mux.HandleFunc("PUT /api/php/settings", s.handleSetPHPSettings)

	// Dumps
	s.mux.HandleFunc("GET /api/dumps", s.handleGetDumps)
	s.mux.HandleFunc("DELETE /api/dumps", s.handleClearDumps)
	s.mux.HandleFunc("GET /ws/dumps", s.handleDumpsWS)

	// TLS
	s.mux.HandleFunc("GET /api/tls/cert", s.handleTLSCert)
	s.mux.HandleFunc("POST /api/tls/trust", s.handleTLSTrust)

	// Settings
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	// Mail — config must be registered before the catch-all proxy.
	s.mux.HandleFunc("GET /api/mail/config", s.handleMailConfig)
	s.mux.HandleFunc("/api/mail/", s.handleMailProxy)
	s.mux.HandleFunc("GET /ws/mail", s.handleMailWS)

	// Serve embedded Vue SPA — must be last.
	s.mux.HandleFunc("/", s.handleSPA)
}

// Listen starts the HTTP server on the given address and shuts down cleanly
// when ctx is cancelled.
func (s *Server) Listen(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("devctl listening on http://%s", ln.Addr())

	srv := &http.Server{Handler: s.mux}

	// Shut down when the context is done.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("devctl: shutdown: %v", err)
		}
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// handleSPA serves the embedded Vue SPA for all non-API routes.
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(s.uiFS, "ui/dist")
	if err != nil {
		http.Error(w, "ui not built", http.StatusInternalServerError)
		return
	}

	fsrv := http.FileServer(http.FS(sub))

	// Try to serve the file; if not found, serve index.html (SPA fallback).
	_, statErr := fs.Stat(sub, r.URL.Path[1:])
	if r.URL.Path != "/" && statErr != nil {
		// Serve index.html for all SPA routes.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fsrv.ServeHTTP(w, r2)
		return
	}

	fsrv.ServeHTTP(w, r)
}

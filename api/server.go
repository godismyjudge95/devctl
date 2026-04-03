package api

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
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
	hooks       *install.HookRegistry
	serverRoot  string // absolute path to the devctl server directory
	siteUser    string // OS username of the non-root site user (e.g. "daniel")
	siteHome    string // home directory of the non-root site user (e.g. "/home/daniel")
	devctlAddr  string // listen address passed to EnsureHTTPServer (e.g. "127.0.0.1:4000")
	mux         *http.ServeMux
	uiFS        embed.FS

	// latestVersions caches the most recently fetched latest version string for
	// each installer, keyed by service ID. Protected by latestVersionsMu.
	latestVersionsMu sync.RWMutex
	latestVersions   map[string]string
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
	hooks *install.HookRegistry,
	uiFS embed.FS,
	serverRoot string,
	siteUser string,
	siteHome string,
	devctlAddr string,
) *Server {
	s := &Server{
		db:             db,
		queries:        dbq.New(db),
		registry:       registry,
		manager:        manager,
		supervisor:     supervisor,
		poller:         poller,
		dumps:          dumpsServer,
		caddy:          caddyClient,
		siteManager:    siteManager,
		installers:     installers,
		hooks:          hooks,
		serverRoot:     serverRoot,
		siteUser:       siteUser,
		siteHome:       siteHome,
		devctlAddr:     devctlAddr,
		mux:            http.NewServeMux(),
		uiFS:           uiFS,
		latestVersions: make(map[string]string),
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
	s.mux.HandleFunc("DELETE /api/services/{id}/logs", s.handleClearServiceLogs)
	s.mux.HandleFunc("GET /api/services/{id}/credentials", s.handleServiceCredentials)
	s.mux.HandleFunc("GET /api/services/{id}/details", s.handleGetServiceDetails)
	s.mux.HandleFunc("GET /api/services/{id}/settings", s.handleGetServiceSettings)
	s.mux.HandleFunc("PUT /api/services/{id}/settings", s.handlePutServiceSettings)
	s.mux.HandleFunc("GET /api/services/{id}/config/{file}", s.handleGetServiceConfig)
	s.mux.HandleFunc("PUT /api/services/{id}/config/{file}", s.handlePutServiceConfig)
	s.mux.HandleFunc("POST /api/services/{id}/update", s.handleServiceUpdate)
	s.mux.HandleFunc("GET /api/services/events", s.handleServiceEvents)

	// Logs
	s.mux.HandleFunc("GET /api/logs", s.handleGetLogs)
	s.mux.HandleFunc("GET /api/logs/{id}/tail", s.handleGetLogTail)
	s.mux.HandleFunc("GET /api/logs/{id}", s.handleGetLogStream)
	s.mux.HandleFunc("DELETE /api/logs/{id}", s.handleClearLog)

	// Sites
	s.mux.HandleFunc("GET /api/sites/detect", s.handleDetectSite)
	s.mux.HandleFunc("POST /api/sites/refresh-metadata", s.handleRefreshSiteMetadata)
	s.mux.HandleFunc("GET /api/sites", s.handleGetSites)
	s.mux.HandleFunc("POST /api/sites", s.handleCreateSite)
	s.mux.HandleFunc("GET /api/sites/{id}", s.handleGetSite)
	s.mux.HandleFunc("PUT /api/sites/{id}", s.handleUpdateSite)
	s.mux.HandleFunc("DELETE /api/sites/{id}", s.handleDeleteSite)
	s.mux.HandleFunc("POST /api/sites/{id}/spx/enable", s.handleSPXEnable)
	s.mux.HandleFunc("POST /api/sites/{id}/spx/disable", s.handleSPXDisable)
	// Worktrees
	s.mux.HandleFunc("GET /api/sites/{id}/branches", s.handleGetSiteBranches)
	s.mux.HandleFunc("GET /api/sites/{id}/worktree-config", s.handleGetWorktreeConfig)
	s.mux.HandleFunc("PUT /api/sites/{id}/worktree-config", s.handlePutWorktreeConfig)
	s.mux.HandleFunc("GET /api/sites/{id}/worktrees", s.handleGetSiteWorktrees)
	s.mux.HandleFunc("POST /api/sites/{id}/worktrees", s.handleCreateWorktree)
	s.mux.HandleFunc("DELETE /api/sites/{id}/worktrees/{worktreeId}", s.handleRemoveWorktree)

	// PHP
	s.mux.HandleFunc("GET /api/php/versions", s.handleGetPHPVersions)
	s.mux.HandleFunc("POST /api/php/versions/{ver}/install", s.handleInstallPHP)
	s.mux.HandleFunc("DELETE /api/php/versions/{ver}", s.handleUninstallPHP)
	s.mux.HandleFunc("POST /api/php/versions/{ver}/start", s.handlePHPFPMStart)
	s.mux.HandleFunc("POST /api/php/versions/{ver}/stop", s.handlePHPFPMStop)
	s.mux.HandleFunc("POST /api/php/versions/{ver}/restart", s.handlePHPFPMRestart)
	s.mux.HandleFunc("GET /api/php/settings", s.handleGetPHPSettings)
	s.mux.HandleFunc("PUT /api/php/settings", s.handleSetPHPSettings)
	s.mux.HandleFunc("GET /api/php/versions/{ver}/config/{file}", s.handleGetPHPConfig)
	s.mux.HandleFunc("PUT /api/php/versions/{ver}/config/{file}", s.handleSetPHPConfig)

	// Dumps
	s.mux.HandleFunc("GET /api/dumps", s.handleGetDumps)
	s.mux.HandleFunc("DELETE /api/dumps", s.handleClearDumps)
	s.mux.HandleFunc("GET /ws/dumps", s.handleDumpsWS)

	// SPX profiler
	s.mux.HandleFunc("GET /api/spx/profiles", s.handleGetSpxProfiles)
	s.mux.HandleFunc("DELETE /api/spx/profiles", s.handleClearSpxProfiles)
	s.mux.HandleFunc("GET /api/spx/profiles/{key}/speedscope", s.handleGetSpxSpeedscope)
	s.mux.HandleFunc("GET /api/spx/profiles/{key}", s.handleGetSpxProfile)
	s.mux.HandleFunc("DELETE /api/spx/profiles/{key}", s.handleDeleteSpxProfile)

	// TLS
	s.mux.HandleFunc("GET /api/tls/cert", s.handleTLSCert)
	s.mux.HandleFunc("POST /api/tls/trust", s.handleTLSTrust)

	// Settings
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("GET /api/settings/resolved", s.handleGetResolvedSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	s.mux.HandleFunc("POST /api/restart", s.handleRestart)

	// DNS
	s.mux.HandleFunc("GET /api/dns/detect-ip", s.handleDNSDetectIP)
	s.mux.HandleFunc("GET /api/dns/setup", s.handleDNSCheckSetup)
	s.mux.HandleFunc("POST /api/dns/setup", s.handleDNSSetup)
	s.mux.HandleFunc("DELETE /api/dns/setup", s.handleDNSTeardown)

	// Mail — config must be registered before the catch-all proxy.
	s.mux.HandleFunc("GET /api/mail/config", s.handleMailConfig)
	s.mux.HandleFunc("/api/mail/", s.handleMailProxy)
	s.mux.HandleFunc("GET /ws/mail", s.handleMailWS)

	// RustFS — presign must be registered before the catch-all proxies.
	s.mux.HandleFunc("GET /api/rustfs/presign", s.handleRustFSPresign)
	s.mux.HandleFunc("/api/rustfs/s3/", s.handleRustFSS3Proxy)
	s.mux.HandleFunc("/api/rustfs/admin/", s.handleRustFSAdminProxy)

	// /_testing/ — debug/test endpoints. Only registered when DEVCTL_TESTING=true.
	// These routes are used by integration tests to inject state without hitting
	// external services (e.g. fake a newer upstream version to trigger update_available).
	if os.Getenv("DEVCTL_TESTING") == "true" {
		s.mux.HandleFunc("POST /_testing/services/{id}/latest-version", s.handleTestingSetLatestVersion)
	}

	// Serve embedded Vue SPA — must be last.
	// Speedscope static assets are served explicitly to prevent the SPA
	// catch-all from intercepting /speedscope/* requests.
	s.mux.HandleFunc("/speedscope/", s.handleSpeedscope)

	// Catch-all for unknown /api/* routes — must be before the SPA handler.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

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

// handleSpeedscope serves the embedded speedscope static assets under /speedscope/.
// A dedicated handler is needed so the SPA catch-all does not intercept these paths.
func (s *Server) handleSpeedscope(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(s.uiFS, "ui/dist/speedscope")
	if err != nil {
		http.Error(w, "speedscope assets not built", http.StatusInternalServerError)
		return
	}
	http.StripPrefix("/speedscope/", http.FileServer(http.FS(sub))).ServeHTTP(w, r)
}
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

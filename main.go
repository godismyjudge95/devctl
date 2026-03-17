package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/danielgormly/devctl/api"
	"github.com/danielgormly/devctl/config"
	"github.com/danielgormly/devctl/db"
	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/dumps"
	"github.com/danielgormly/devctl/install"
	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/selfinstall"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
// It defaults to "dev" for local builds.
var version = "dev"

//go:embed ui/dist
var uiFS embed.FS

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "version":
			fmt.Println(version)
			return
		case "install":
			if err := selfinstall.Run(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "devctl install: %v\n", err)
				os.Exit(1)
			}
			return
		case "uninstall":
			if err := selfinstall.Uninstall(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "devctl uninstall: %v\n", err)
				os.Exit(1)
			}
			return
		case "open":
			if err := runOpen(); err != nil {
				fmt.Fprintf(os.Stderr, "devctl open: %v\n", err)
				os.Exit(1)
			}
			return
		case "--help", "-h", "help":
			fmt.Print(usage)
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "devctl: %v\n", err)
		os.Exit(1)
	}
}

const usage = `Usage: devctl <command> [flags]

Commands:
  (no command)   Start the devctl daemon (requires root)
  open           Open the current project's .test URL in the browser
  install        Install devctl as a systemd service
  uninstall      Remove the devctl systemd service
  version        Print the version and exit
  --version      Print the version and exit

install flags:
  --user         Non-root user whose sites dir devctl will manage
                 (auto-detected from SUDO_USER if omitted)
  --sites-dir    Directory where sites are stored (default: ~/sites)
  --path         Directory to install the devctl binary into
                 (default: ~/sites/server/devctl)
  --yes          Skip all confirmation prompts (for scripted installs)

uninstall flags:
  --yes          Skip all confirmation prompts, remove binary and config

`

func run() error {
	t0 := time.Now()
	step := func(name string) func() {
		log.Printf("startup: %s ...", name)
		return func() { log.Printf("startup: %s done (%s)", name, time.Since(t0).Round(time.Millisecond)) }
	}

	// --- Root check ---
	if os.Getuid() != 0 {
		fmt.Fprintln(os.Stderr, "devctl: must be run as root. Re-run with: sudo devctl")
		os.Exit(1)
	}

	// --- Config ---
	done := step("config")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	done()

	// --- Database ---
	done = step("database open")
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()
	done()

	// --- Load runtime settings from DB ---
	done = step("settings")
	queries := dbq.New(database)
	ctx := context.Background()

	caddyAdminURL := "http://localhost:2019"
	dumpTCPPort := ":" + getSetting(ctx, queries, database, "dump_tcp_port", "9912")
	sitesWatchDir := getSetting(ctx, queries, database, "sites_watch_dir", "")
	if sitesWatchDir == "" {
		sitesWatchDir = filepath.Dir(cfg.ServerRoot)
	}
	pollIntervalSec := getSetting(ctx, queries, database, "service_poll_interval", "5")
	pollInterval := parseDuration(pollIntervalSec)
	listenHost := getSetting(ctx, queries, database, "devctl_host", "127.0.0.1")
	listenPort := getSetting(ctx, queries, database, "devctl_port", "4000")
	addr := listenHost + ":" + listenPort
	done()

	// --- PHP prepend setup ---
	// Write prepend.php to disk on every startup — idempotent and harmless.
	// Must run before WriteConfigs so the file exists when FPM starts.
	done = step("php prepend")
	if err := php.InstallPrepend(cfg.ServerRoot); err != nil {
		log.Printf("php: install prepend: %v", err)
	}
	done()

	// --- PHP-FPM config refresh ---
	// Re-write php.ini and php-fpm.conf for every installed version on startup.
	// This ensures the pool user/group and auto_prepend_file are always current
	// (e.g. if the site user or server root changed since the last install).
	// InstallPrepend must run first so the prepend.php file exists on disk.
	done = step("php-fpm config refresh")
	if phpVersions, err := php.InstalledVersions(cfg.ServerRoot); err == nil {
		for _, v := range phpVersions {
			if err := php.WriteConfigs(v.Version, cfg.ServerRoot, cfg.SiteUser); err != nil {
				log.Printf("php: refresh config for %s: %v", v.Version, err)
			}
		}
	}
	done()

	// --- Services ---
	done = step("services registry")
	registry := services.NewRegistry(config.DefaultServices(cfg.ServerRoot, cfg.SiteUser))
	done()

	supervisor := services.NewSupervisor(cfg.ServerRoot)
	manager := services.NewManager(registry, supervisor)
	poller := services.NewPoller(registry, manager, pollInterval)

	// --- PHP-FPM: register installed versions as supervised service definitions ---
	done = step("php-fpm registry")
	if phpVersions, err := php.InstalledVersions(cfg.ServerRoot); err == nil {
		for _, v := range phpVersions {
			registry.Register(phpFPMDefinition(v.Version, cfg.ServerRoot))
		}
	} else {
		log.Printf("php: scan installed versions: %v", err)
	}
	done()

	// Auto-install + auto-start Caddy first so the Admin API is ready before EnsureHTTPServer.
	// installRegistry isn't built yet at this point, so we instantiate CaddyInstaller directly.
	caddyInstaller := install.NewCaddyInstaller(supervisor, cfg.ServerRoot)
	if !caddyInstaller.IsInstalled() {
		log.Printf("startup: caddy not installed — installing now...")
		if err := caddyInstaller.Install(ctx); err != nil {
			log.Printf("startup: caddy install failed: %v", err)
		} else {
			log.Printf("startup: caddy installed successfully")
		}
	}
	for _, def := range registry.All() {
		if def.Managed && def.ID == "caddy" {
			if _, statErr := os.Stat(def.ManagedCmd); statErr == nil {
				if err := supervisor.Start(def); err != nil {
					log.Printf("supervisor: auto-start %s: %v", def.ID, err)
				}
			}
		}
	}
	// Auto-start all installed PHP-FPM versions.
	for _, def := range registry.All() {
		if def.Managed && isPHPFPMDef(def) {
			if _, statErr := os.Stat(def.ManagedCmd); statErr == nil {
				if err := supervisor.Start(def); err != nil {
					log.Printf("supervisor: auto-start %s: %v", def.ID, err)
				}
			}
		}
	}
	done = step("caddy ensure http server")
	caddyClient := sites.NewCaddyClient(caddyAdminURL)
	siteManager := sites.NewManager(database, caddyClient, cfg.ServerRoot)

	// Wait for the Caddy Admin API to be ready (up to 10 s) before pushing config.
	// This is necessary when Caddy is a supervised child process.
	if err := caddyClient.WaitForAdmin(10 * time.Second); err != nil {
		log.Printf("caddy: admin api not ready: %v", err)
	}

	// Ensure Caddy has the devctl HTTP server block before syncing sites.
	if err := caddyClient.EnsureHTTPServer(addr); err != nil {
		log.Printf("caddy: ensure http server: %v (Caddy may not be running yet)", err)
	}
	done()

	done = step("sites sync")
	if err := siteManager.SyncAll(ctx); err != nil {
		log.Printf("sites: startup sync: %v", err)
	}
	siteManager.RemoveServerSite(ctx)
	done()

	done = step("watcher")
	watcher, err := sites.NewWatcher(siteManager)
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	done()

	// --- Dumps ---
	dumpsServer := dumps.NewServer(database, 500)

	// --- Install registry ---
	// Build after siteManager and supervisor are ready so ReverbInstaller
	// can receive its dependencies.
	installRegistry := install.NewRegistry(siteManager, queries, supervisor, cfg.SiteUser, cfg.ServerRoot, cfg.SiteHome)

	// Wire installer IsInstalled checks into the manager so GetState works.
	for id, inst := range installRegistry {
		inst := inst // capture
		manager.SetInstallerCheck(id, inst.IsInstalled)
	}

	// --- HTTP Server ---
	// Build before auto-start loops so srv.ServiceDef() can apply DB settings
	// (e.g. dns port/target-ip) to the definitions used by the supervisor.
	log.Printf("startup: total init %s — listening on %s", time.Since(t0).Round(time.Millisecond), addr)
	srv := api.NewServer(database, registry, manager, supervisor, poller, dumpsServer, caddyClient, siteManager, installRegistry, uiFS, cfg.ServerRoot, cfg.SiteUser, addr)

	// Auto-start remaining installed managed services (all except caddy, already started).
	for _, def := range registry.All() {
		if def.Managed && def.ID != "caddy" {
			if inst, ok := installRegistry[def.ID]; ok && inst.IsInstalled() {
				if err := supervisor.Start(srv.ServiceDef(ctx, def)); err != nil {
					log.Printf("supervisor: auto-start %s: %v", def.ID, err)
				}
			}
		}
	}

	// Auto-start PHP-FPM supervised services for all registered versions.
	for _, def := range registry.All() {
		if def.Managed && isPHPFPMDef(def) {
			if _, statErr := os.Stat(def.ManagedCmd); statErr == nil {
				if err := supervisor.Start(def); err != nil {
					log.Printf("supervisor: auto-start %s: %v", def.ID, err)
				}
			}
		}
	}

	// --- Start background goroutines ---
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go poller.Run(runCtx)
	go supervisor.Run(runCtx)

	go func() {
		if err := watcher.Watch(runCtx, sitesWatchDir); err != nil {
			log.Printf("watcher: %v", err)
		}
	}()

	go func() {
		if err := dumpsServer.Run(runCtx, dumpTCPPort); err != nil {
			log.Printf("dumps: %v", err)
		}
	}()

	// --- Graceful shutdown ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("devctl: shutting down...")
		cancel()
	}()

	// --- Listen ---
	return srv.Listen(runCtx, addr)
}

// getSetting retrieves a setting from the DB with a fallback default.
func getSetting(ctx context.Context, q *dbq.Queries, _ *sql.DB, key, def string) string {
	v, err := q.GetSetting(ctx, key)
	if err != nil || v == "" {
		return def
	}
	return v
}

// parseDuration converts a seconds string to time.Duration, defaulting to 5s.
func parseDuration(s string) time.Duration {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 5 * time.Second
}

// phpFPMDefinition builds a supervised services.Definition for a PHP-FPM version.
// The process is run as: php-fpm --nodaemonize --fpm-config {dir}/php-fpm.conf
// --nodaemonize keeps it in the foreground so the supervisor can own the lifecycle.
func phpFPMDefinition(ver, serverRoot string) services.Definition {
	return services.Definition{
		ID:           php.FPMServiceID(ver),
		Label:        "PHP " + ver + " FPM",
		Description:  "PHP " + ver + " FastCGI Process Manager",
		Managed:      true,
		ManagedCmd:   php.FPMBinary(ver, serverRoot),
		ManagedArgs:  fmt.Sprintf("--nodaemonize --fpm-config %s", php.FPMConfigPath(ver, serverRoot)),
		ManagedDir:   php.PHPDir(ver, serverRoot),
		Log:          php.FPMLogPath(ver, serverRoot),
		Version:      php.FPMBinary(ver, serverRoot) + " -v",
		VersionRegex: `PHP (?P<version>[\d.]+)`,
	}
}

// isPHPFPMDef reports whether a Definition was created by phpFPMDefinition.
func isPHPFPMDef(def services.Definition) bool {
	return strings.HasPrefix(def.ID, "php-fpm-")
}

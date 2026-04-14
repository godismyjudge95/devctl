package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/danielgormly/devctl/api"
	"github.com/danielgormly/devctl/cli"
	"github.com/danielgormly/devctl/config"
	"github.com/danielgormly/devctl/db"
	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/dumps"
	"github.com/danielgormly/devctl/install"
	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/selfinstall"
	"github.com/danielgormly/devctl/selfupdate"
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
		case "daemon":
			if err := run(); err != nil {
				fmt.Fprintf(os.Stderr, "devctl: %v\n", err)
				os.Exit(1)
			}
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
		case "path-setup":
			if err := runPathSetup(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "devctl path-setup: %v\n", err)
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
			cli.PrintHelp()
			return
		default:
			// Dispatch colon-namespaced CLI commands (e.g. services:restart caddy)
			if cli.Dispatch(os.Args[1:]) {
				return
			}
			fmt.Fprintf(os.Stderr, "devctl: unknown command %q\n\nRun `devctl help` for a list of commands.\n", os.Args[1])
			os.Exit(1)
		}
	}
	// No subcommand: print help
	cli.PrintHelp()
}

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

	// --- Self-update backup cleanup ---
	// If the binary was just updated, a .bak file from the previous version
	// exists next to the current binary. Remove it now that we've successfully
	// started, proving the update was good.
	if exe, err := os.Executable(); err == nil {
		selfupdate.CleanupBackup(exe)
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

	// --- Service config migration ---
	// Ensure native config files exist for services that were installed before
	// config-file support was added. Each Ensure* call is a no-op if the file
	// already exists, so this is safe to run on every startup.
	done = step("service config migration")
	if err := install.EnsureValkeyConf(cfg.ServerRoot); err != nil {
		log.Printf("startup: valkey config: %v", err)
	}
	if err := install.EnsureMeilisearchConf(cfg.ServerRoot); err != nil {
		log.Printf("startup: meilisearch config: %v", err)
	}
	if err := install.EnsureTypesenseConf(cfg.ServerRoot); err != nil {
		log.Printf("startup: typesense config: %v", err)
	}
	if err := install.EnsureMySQLPlugins(cfg.ServerRoot); err != nil {
		log.Printf("startup: mysql plugins: %v", err)
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
	installRegistry, installHooks := install.NewRegistry(siteManager, queries, supervisor, cfg.SiteUser, cfg.ServerRoot, cfg.SiteHome)

	// Wire installer IsInstalled checks into the manager so GetState works.
	for id, inst := range installRegistry {
		inst := inst // capture
		manager.SetInstallerCheck(id, inst.IsInstalled)
	}

	// --- HTTP Server ---
	// Build before auto-start loops so srv.ServiceDef() can apply DB settings
	// (e.g. dns port/target-ip) to the definitions used by the supervisor.
	log.Printf("startup: total init %s — listening on %s", time.Since(t0).Round(time.Millisecond), addr)
	srv := api.NewServer(database, registry, manager, supervisor, poller, dumpsServer, caddyClient, siteManager, installRegistry, installHooks, uiFS, cfg.ServerRoot, cfg.SiteUser, cfg.SiteHome, addr, version)

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

	// --- Update checker ---
	// Runs once per day at 3am.
	go runUpdateChecker(runCtx, srv, installRegistry)

	// --- Self-update checker ---
	// Checks GitHub for a newer devctl release once per day at 3am.
	go runSelfUpdateChecker(runCtx, srv)

	// --- Skill auto-update ---
	// If the user has the devctl CLI skill installed, regenerate it silently.
	go cli.UpdateSkillIfInstalled()

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
// The process is run as: php-fpm -c {dir}/php.ini --nodaemonize --fpm-config {dir}/php-fpm.conf
// -c loads the per-version php.ini so SPX and other ini settings take effect in workers.
// --nodaemonize keeps it in the foreground so the supervisor can own the lifecycle.
func phpFPMDefinition(ver, serverRoot string) services.Definition {
	phpDir := php.PHPDir(ver, serverRoot)
	return services.Definition{
		ID:           php.FPMServiceID(ver),
		Label:        "PHP " + ver + " FPM",
		Description:  "PHP " + ver + " FastCGI Process Manager",
		Managed:      true,
		ManagedCmd:   php.FPMBinary(ver, serverRoot),
		ManagedArgs:  fmt.Sprintf("-c %s/php.ini --nodaemonize --fpm-config %s", phpDir, php.FPMConfigPath(ver, serverRoot)),
		ManagedDir:   phpDir,
		Log:          php.FPMLogPath(ver, serverRoot),
		Version:      php.FPMBinary(ver, serverRoot) + " -v",
		VersionRegex: `PHP (?P<version>[\d.]+)`,
	}
}

// isPHPFPMDef reports whether a Definition was created by phpFPMDefinition.
func isPHPFPMDef(def services.Definition) bool {
	return strings.HasPrefix(def.ID, "php-fpm-")
}

// runUpdateChecker checks for updates for all installed services once per day
// at 3am. When a newer version is found it stores the result in srv so the API
// can surface it to the frontend.
func runUpdateChecker(ctx context.Context, srv *api.Server, installers map[string]install.Installer) {
	check := func() {
		log.Printf("update-checker: checking for updates...")
		for id, inst := range installers {
			if !inst.IsInstalled() {
				continue
			}
			latest, err := inst.LatestVersion(ctx)
			if err != nil {
				log.Printf("update-checker: %s: %v", id, err)
				continue
			}
			if latest == "" {
				continue // version check not supported for this service
			}
			srv.SetLatestVersion(id, latest)
		}
		log.Printf("update-checker: done")
	}

	for {
		now := time.Now()
		next3am := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 0, 0, 0, now.Location())
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next3am)):
			check()
		}
	}
}

// runSelfUpdateChecker checks GitHub for a newer devctl release once per day
// at 3am. When a newer version is found it stores the result in srv so the API
// can surface it to the frontend.
func runSelfUpdateChecker(ctx context.Context, srv *api.Server) {
	check := func() {
		latest, err := selfupdate.LatestVersion(ctx)
		if err != nil {
			log.Printf("self-update-checker: %v", err)
			return
		}
		srv.SetSelfLatestVersion(latest)
	}

	for {
		now := time.Now()
		next3am := time.Date(now.Year(), now.Month(), now.Day()+1, 3, 0, 0, 0, now.Location())
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next3am)):
			check()
		}
	}
}

// runPathSetup implements the `devctl path-setup` sub-command. It writes or
// removes the devctl PATH block in the user's shell config files. This is
// called by the Makefile so that the install/deploy targets don't need to
// duplicate the shell-detection logic.
func runPathSetup(args []string) error {
	fs := flag.NewFlagSet("devctl path-setup", flag.ContinueOnError)
	flagBinDir := fs.String("bin-dir", "", "bin directory to add to PATH")
	flagHome := fs.String("home", "", "home directory of the target user")
	flagUser := fs.String("user", "", "username of the target user")
	flagRemove := fs.Bool("remove", false, "remove the PATH block instead of writing it")
	fs.SetOutput(os.Stderr)

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *flagRemove {
		if *flagHome == "" {
			return errors.New("--home is required")
		}
		selfinstall.RemovePATHSetup(*flagHome)
		return nil
	}

	if *flagBinDir == "" {
		return errors.New("--bin-dir is required")
	}
	if *flagHome == "" {
		return errors.New("--home is required")
	}
	if *flagUser == "" {
		return errors.New("--user is required")
	}

	u, err := user.Lookup(*flagUser)
	if err != nil {
		return fmt.Errorf("lookup user %q: %w", *flagUser, err)
	}
	var uid, gid int
	if _, err := fmt.Sscan(u.Uid, &uid); err != nil {
		return fmt.Errorf("parse uid %q: %w", u.Uid, err)
	}
	if _, err := fmt.Sscan(u.Gid, &gid); err != nil {
		return fmt.Errorf("parse gid %q: %w", u.Gid, err)
	}
	return selfinstall.WritePATHSetup(*flagBinDir, *flagHome, *flagUser, uid, gid)
}

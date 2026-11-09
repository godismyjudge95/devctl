package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/dist"
	"github.com/danielgormly/devctl/doctor"
	"github.com/danielgormly/devctl/elevate"
	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/php"
	"github.com/danielgormly/devctl/selfinstall"
	"github.com/danielgormly/devctl/sites"
)

const lowDiskBytes = 2 << 30

// dashboardHost is the vhost EnsureHTTPServer always configures, so a probe
// for it reaches Caddy whether or not the user has sites.
const dashboardHost = "devctl.test"

// doctorChecks is the check registry. Order matters for doctor:fix: a fix
// that starts Caddy runs before the port and CA checks that need it.
func (s *Server) doctorChecks() []doctor.Check {
	return []doctor.Check{
		{ID: "elevate-daemon", Title: "Elevate daemon running", Run: s.checkElevateDaemon},
		{ID: "caddy", Title: "Caddy running", Run: s.checkCaddy},
		{ID: "port-http", Title: "Port 80 served by Caddy", Run: s.checkPortHTTP},
		{ID: "port-https", Title: "Port 443 served by Caddy", Run: s.checkPortHTTPS},
		{ID: "dns-server", Title: "DNS server answers", Run: s.checkDNSServer},
		{ID: "resolver", Title: "OS resolver routes the TLD", Run: s.checkResolver},
		{ID: "ca-trust", Title: "Caddy CA trusted by the OS", Run: s.checkCATrust},
		{ID: "php-ca-bundle", Title: "PHP CA bundle current", Run: s.checkPHPCABundle},
		{ID: "php-installed", Title: "PHP installed", Run: s.checkPHPInstalled},
		{ID: "php-fpm", Title: "PHP-FPM pools running", Run: s.checkPHPFPM},
		{ID: "site-php", Title: "Sites use an installed PHP", Run: s.checkSitePHP},
		{ID: "site-roots", Title: "Site directories exist", Run: s.checkSiteRoots},
		{ID: "shell-path", Title: "devctl bin directory on PATH", Run: s.checkShellPath},
		{ID: "service-configs", Title: "Service config files parse", Run: s.checkServiceConfigs},
		{ID: "devctl-update", Title: "devctl up to date", Run: s.checkSelfUpdate},
		{ID: "disk-space", Title: "Disk space", Run: s.checkDiskSpace},
	}
}

// handleGetDoctor runs every check.
//
//	GET /api/doctor
func (s *Server) handleGetDoctor(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, doctor.Results(doctor.Run(r.Context(), s.doctorChecks())))
}

// handleDoctorFix applies the safe fixes the checks offer, then re-runs the
// checks. Body {"ids": [...]} limits the fixes to those checks.
//
//	POST /api/doctor/fix
func (s *Server) handleDoctorFix(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	checks := s.doctorChecks()
	known := make(map[string]bool, len(checks))
	for _, c := range checks {
		known[c.ID] = true
	}
	only := map[string]bool{}
	for _, id := range body.IDs {
		if !known[id] {
			writeError(w, fmt.Sprintf("unknown check %q", id), http.StatusBadRequest)
			return
		}
		only[id] = true
	}
	fixes := doctor.ApplyFixes(r.Context(), doctor.Run(r.Context(), checks), only)
	writeJSON(w, doctor.FixReport{Fixes: fixes, Report: doctor.Results(doctor.Run(r.Context(), checks))})
}

func (s *Server) setting(ctx context.Context, key string) string {
	if v, err := s.queries.GetSetting(ctx, key); err == nil && v != "" {
		return v
	}
	return settingDefaults[key]
}

// probeDomain is a name inside the first configured TLD (e.g. "devctl.test").
func (s *Server) probeDomain(ctx context.Context) string {
	tld := strings.TrimSpace(strings.Split(s.setting(ctx, "dns_tld"), ",")[0])
	return "devctl." + strings.TrimPrefix(tld, ".")
}

func (s *Server) checkElevateDaemon(ctx context.Context) doctor.Finding {
	const remedy = "sudo devctl elevate ports"
	err := s.elevateClient.Ping(ctx)
	switch {
	case err == nil:
		return doctor.Okf("Answers on %s", paths.ElevateSocket(s.serverRoot))
	case !elevate.ElevateDaemonConfigured():
		return doctor.Failf(remedy, "Not installed. Caddy cannot bind ports 80 and 443 without it")
	default:
		return doctor.Failf(remedy, "Installed but not answering: %v", err)
	}
}

func (s *Server) checkCaddy(ctx context.Context) doctor.Finding {
	def, ok := s.registry.Get("caddy")
	if !ok {
		return doctor.Failf("", "Caddy is not registered")
	}
	if _, err := os.Stat(def.ManagedCmd); err != nil {
		return doctor.Failf("Install Caddy from Services in the dashboard", "Caddy is not installed")
	}
	if err := s.caddy.WaitForAdmin(time.Second); err != nil {
		state := "the process is running"
		if !s.supervisor.IsRunning("caddy") {
			state = "Caddy is not running"
		}
		f := doctor.Failf("", "Admin API at %s does not answer; %s", s.caddy.AdminURL(), state)
		f.Fix = func(ctx context.Context) error {
			if err := s.manager.Restart(s.serviceDef(ctx, def)); err != nil {
				return err
			}
			if err := s.caddy.WaitForAdmin(10 * time.Second); err != nil {
				return fmt.Errorf("caddy admin api: %w", err)
			}
			s.pushCaddyConfig("doctor fix")
			return nil
		}
		return f
	}
	if !s.caddy.HasHTTPServer() {
		f := doctor.Warnf("", "Caddy runs but has no devctl server config, so no site is served")
		f.Fix = func(context.Context) error {
			s.pushCaddyConfig("doctor fix")
			if !s.caddy.HasHTTPServer() {
				return errors.New("caddy config push did not take; see the caddy log")
			}
			return nil
		}
		return f
	}
	return doctor.Okf("Running; admin API at %s", s.caddy.AdminURL())
}

// portOwnerHint names the OS command that shows which program holds a port.
func portOwnerHint(port int) string {
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("Find the program with `sudo lsof -nP -iTCP:%d -sTCP:LISTEN` and stop it", port)
	}
	return fmt.Sprintf("Find the program with `sudo ss -ltnp 'sport = :%d'` and stop it", port)
}

func (s *Server) checkPortHTTP(ctx context.Context) doctor.Finding {
	port := dist.HTTPPort()
	server, err := doctor.HTTPServerHeader(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), dashboardHost)
	if err != nil {
		return doctor.Failf("sudo devctl elevate ports", "Nothing answers HTTP on port %d: %v", port, err)
	}
	if server != "Caddy" {
		return doctor.Failf(portOwnerHint(port), "Another program answers on port %d (Server: %q)", port, server)
	}
	return doctor.Okf("Caddy answers on port %d", port)
}

func (s *Server) checkPortHTTPS(ctx context.Context) doctor.Finding {
	port := dist.HTTPSPort()
	issuer, err := doctor.TLSIssuer(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), dashboardHost)
	if err != nil {
		return doctor.Failf("sudo devctl elevate ports", "No TLS server answers on port %d: %v", port, err)
	}
	if !doctor.IsCaddyIssuer(issuer) {
		return doctor.Failf(portOwnerHint(port), "Port %d presents a certificate from %q, not Caddy's local CA", port, issuer)
	}
	return doctor.Okf("Caddy answers on port %d", port)
}

func (s *Server) checkDNSServer(ctx context.Context) doctor.Finding {
	addr := net.JoinHostPort("127.0.0.1", s.setting(ctx, "dns_port"))
	name := s.probeDomain(ctx)
	ips, err := doctor.ResolveA(ctx, addr, name)
	if err == nil {
		return doctor.Okf("%s answers %s with %s", addr, name, ips[0])
	}
	f := doctor.Failf("", "%s does not answer %s: %v", addr, name, err)
	if def, ok := s.registry.Get("dns"); ok {
		f.Fix = func(ctx context.Context) error {
			return s.manager.Restart(s.serviceDef(ctx, def))
		}
	}
	return f
}

func (s *Server) checkResolver(ctx context.Context) doctor.Finding {
	name := s.probeDomain(ctx)
	if elevate.ResolverConfigured() {
		return doctor.Okf("OS resolver stub sends %s to devctl", name)
	}
	if _, err := net.DefaultResolver.LookupHost(ctx, name); err == nil {
		return doctor.Okf("No devctl resolver stub, but the system DNS resolves %s", name)
	}
	return doctor.Warnf("sudo devctl elevate resolver", "No resolver stub and the system DNS does not resolve %s", name)
}

func (s *Server) checkCATrust(ctx context.Context) doctor.Finding {
	root, err := s.caddy.RootCert()
	if err != nil {
		return doctor.Warnf("", "Cannot read the Caddy CA (is Caddy running?): %v", err)
	}
	fp, err := elevate.ParseCAFingerprint(root)
	if err != nil {
		return doctor.Warnf("", "Cannot parse the Caddy CA: %v", err)
	}
	if !elevate.CATrusted(fp) {
		return doctor.Warnf("sudo devctl elevate trust", "Browsers will warn on https://*.test until the CA is trusted")
	}
	return doctor.Okf("Trusted (%s)", elevate.InstallCAPath())
}

func (s *Server) checkPHPCABundle(ctx context.Context) doctor.Finding {
	bundlePath := paths.CABundlePath(s.serverRoot)
	rebuild := func(context.Context) error {
		chain, _ := s.caddy.CAChain()
		return php.ApplyCABundle(s.serverRoot, chain)
	}
	bundle, err := os.ReadFile(bundlePath)
	if err != nil {
		f := doctor.Warnf("", "%s is missing; PHP HTTPS calls to *.test will fail", bundlePath)
		f.Fix = rebuild
		return f
	}
	root, err := s.caddy.RootCert()
	if err != nil {
		return doctor.Okf("Present (not compared: Caddy CA unavailable)")
	}
	if !doctor.BundleHasCert(bundle, root) {
		f := doctor.Warnf("", "%s lacks the current Caddy CA; PHP HTTPS calls to *.test will fail", bundlePath)
		f.Fix = rebuild
		return f
	}
	return doctor.Okf("Contains the current Caddy CA")
}

func (s *Server) checkPHPInstalled(ctx context.Context) doctor.Finding {
	versions, err := php.InstalledVersions(s.serverRoot)
	if err != nil {
		return doctor.Failf("", "Cannot list PHP versions: %v", err)
	}
	if len(versions) == 0 {
		return doctor.Failf("Install PHP from Services → Install in the dashboard", "No PHP version is installed")
	}
	names := make([]string, len(versions))
	for i, v := range versions {
		names[i] = v.Version
	}
	return doctor.Okf("PHP %s", strings.Join(names, ", "))
}

func (s *Server) checkPHPFPM(ctx context.Context) doctor.Finding {
	versions, err := php.InstalledVersions(s.serverRoot)
	if err != nil {
		return doctor.Failf("", "Cannot list PHP versions: %v", err)
	}
	var down []string
	for _, v := range versions {
		if !s.supervisor.IsRunning(php.FPMServiceID(v.Version)) {
			down = append(down, v.Version)
		}
	}
	if len(versions) == 0 {
		return doctor.Okf("No pools (no PHP installed)")
	}
	if len(down) == 0 {
		return doctor.Okf("%d of %d running", len(versions), len(versions))
	}
	f := doctor.Failf("", "%d of %d pools are not running", len(down), len(versions))
	for _, v := range down {
		f.Details = append(f.Details, "PHP "+v+" ("+php.FPMLogPath(v, s.serverRoot)+")")
	}
	f.Fix = func(context.Context) error {
		var errs []error
		for _, v := range down {
			if err := s.supervisor.Restart(s.phpFPMServiceDef(v)); err != nil {
				errs = append(errs, fmt.Errorf("php %s: %w", v, err))
			}
		}
		return errors.Join(errs...)
	}
	return f
}

// phpSites returns the user's PHP sites (not service vhosts or proxies).
func (s *Server) phpSites(ctx context.Context) ([]dbq.Site, error) {
	all, err := s.queries.GetAllSites(ctx)
	if err != nil {
		return nil, err
	}
	var out []dbq.Site
	for _, site := range all {
		if site.ServiceVhost == 1 || sites.ProxyFromSettings(site.Settings) != nil {
			continue
		}
		out = append(out, site)
	}
	return out, nil
}

func (s *Server) checkSitePHP(ctx context.Context) doctor.Finding {
	sites, err := s.phpSites(ctx)
	if err != nil {
		return doctor.Failf("", "Cannot list sites: %v", err)
	}
	versions, _ := php.InstalledVersions(s.serverRoot)
	installed := map[string]bool{}
	for _, v := range versions {
		installed[v.Version] = true
	}
	var bad []string
	for _, site := range sites {
		switch {
		case site.PhpVersion == "":
			bad = append(bad, site.Domain+" has no PHP version")
		case !installed[site.PhpVersion]:
			bad = append(bad, fmt.Sprintf("%s uses PHP %s", site.Domain, site.PhpVersion))
		}
	}
	if len(bad) == 0 {
		return doctor.Okf("%d sites checked", len(sites))
	}
	f := doctor.Failf("devctl sites:php <domain> <installed version>", "%d sites have no installed PHP version", len(bad))
	f.Details = bad
	return f
}

func (s *Server) checkSiteRoots(ctx context.Context) doctor.Finding {
	sites, err := s.phpSites(ctx)
	if err != nil {
		return doctor.Failf("", "Cannot list sites: %v", err)
	}
	var bad []string
	for _, site := range sites {
		if _, err := os.Stat(site.RootPath); err != nil {
			bad = append(bad, site.Domain+": "+site.RootPath)
		}
	}
	if len(bad) == 0 {
		return doctor.Okf("%d sites checked", len(sites))
	}
	f := doctor.Failf("Restore the directory, or delete the site in the dashboard", "%d site directories are missing", len(bad))
	f.Details = bad
	return f
}

func (s *Server) checkShellPath(ctx context.Context) doctor.Finding {
	binDir := paths.BinDir(s.serverRoot)
	if files := doctor.ShellPathFiles(s.siteHome, binDir); len(files) > 0 {
		return doctor.Okf("Added to PATH in %s", strings.Join(files, ", "))
	}
	f := doctor.Warnf("Open a new terminal after the fix", "No shell config file adds %s to PATH, so php, composer, and mysql wrappers are not found", binDir)
	f.Fix = func(context.Context) error {
		if err := selfinstall.WritePATHSetup(binDir, s.siteHome, s.siteUser, os.Getuid(), os.Getgid()); err != nil {
			return err
		}
		if len(doctor.ShellPathFiles(s.siteHome, binDir)) == 0 {
			return errors.New("login shell not recognised; add " + binDir + " to PATH by hand")
		}
		return nil
	}
	return f
}

func (s *Server) checkServiceConfigs(ctx context.Context) doctor.Finding {
	var bad []string
	checked := 0
	clickhouseDir := paths.ServiceDir(s.serverRoot, "clickhouse")
	for _, name := range []string{"config.xml", "users.xml"} {
		p := filepath.Join(clickhouseDir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		checked++
		if err := doctor.XMLWellFormed(p); err != nil {
			bad = append(bad, p+": "+err.Error())
		}
	}
	versions, _ := php.InstalledVersions(s.serverRoot)
	for _, v := range versions {
		checked++
		cmd := exec.CommandContext(ctx, php.FPMBinary(v.Version, s.serverRoot), "-t",
			"-c", php.PHPIniPath(v.Version, s.serverRoot),
			"--fpm-config", php.FPMConfigPath(v.Version, s.serverRoot))
		if out, err := cmd.CombinedOutput(); err != nil {
			bad = append(bad, fmt.Sprintf("PHP %s: %s", v.Version, lastLine(string(out), err)))
		}
	}
	if len(bad) == 0 {
		return doctor.Okf("%d config files checked", checked)
	}
	f := doctor.Failf("Fix the file in Services → settings, then restart the service", "%d config files do not parse", len(bad))
	f.Details = bad
	return f
}

func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

func (s *Server) checkSelfUpdate(ctx context.Context) doctor.Finding {
	if s.selfUpdateAvailable() {
		return doctor.Warnf("devctl devctl:update", "devctl %s is available (running %s)", s.GetSelfLatestVersion(), s.selfVersion)
	}
	if s.GetSelfLatestVersion() == "" {
		return doctor.Okf("Running %s (no release check yet)", s.selfVersion)
	}
	return doctor.Okf("Running %s", s.selfVersion)
}

func (s *Server) checkDiskSpace(ctx context.Context) doctor.Finding {
	free, err := doctor.FreeBytes(s.serverRoot)
	if err != nil {
		return doctor.Warnf("", "Cannot read free space on %s: %v", s.serverRoot, err)
	}
	if free < lowDiskBytes {
		return doctor.Warnf("Free space on the disk that holds "+s.serverRoot, "Only %s free on %s", doctor.HumanBytes(free), s.serverRoot)
	}
	return doctor.Okf("%s free on %s", doctor.HumanBytes(free), s.serverRoot)
}

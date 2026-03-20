// Package install provides idempotent install and purge routines for every
// service devctl can manage (Caddy, Redis, PostgreSQL, MySQL, Typesense,
// Meilisearch, Mailpit).
//
// All functions run as root (devctl itself requires root) so no sudo wrapping
// is needed.  Operations that require network access are guarded by a generous
// 10-minute timeout; APT operations use DEBIAN_FRONTEND=noninteractive.
package install

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

const (
	aptTimeout  = 10 * time.Minute
	netTimeout  = 5 * time.Minute
	curlTimeout = 5 * time.Minute
)

// Installer is implemented by each service installer.
type Installer interface {
	// ServiceID returns the services.yaml id for this service (e.g. "caddy").
	ServiceID() string
	// IsInstalled returns true when the service's primary binary is present.
	IsInstalled() bool
	// Install performs an idempotent install (adds APT source, installs
	// packages, enables + starts the service).
	Install(ctx context.Context) error
	// Purge completely removes the service: stops it, purges packages, and
	// removes APT source / keyring files.
	Purge(ctx context.Context) error
	// InstallW is like Install but writes command output to w in real-time.
	InstallW(ctx context.Context, w io.Writer) error
	// PurgeW is like Purge but writes command output to w in real-time.
	// If preserveData is true, service data directories are left intact.
	PurgeW(ctx context.Context, w io.Writer, preserveData bool) error
	// LatestVersion queries the upstream release source and returns the latest
	// available version string (e.g. "v2.10.0"). Returns ("", nil) for services
	// that do not support version checks (e.g. built-in DNS).
	LatestVersion(ctx context.Context) (string, error)
	// UpdateW stops the service, replaces its binary with the latest version,
	// and restarts it. Progress is written to w in real-time. It is a no-op
	// when the service is already at the latest version.
	UpdateW(ctx context.Context, w io.Writer) error
}

// ServiceEvent represents a lifecycle event for an installed service.
type ServiceEvent int

const (
	// EventInstalled fires after a service is successfully installed.
	EventInstalled ServiceEvent = iota
	// EventPurged fires after a service is successfully purged.
	EventPurged
)

// HookFunc is called when a service lifecycle event occurs.
type HookFunc func(id string, event ServiceEvent)

// HookRegistry is a simple registry of lifecycle hooks that are fired when
// services are installed or purged. It is safe for concurrent registration
// and dispatch.
type HookRegistry struct {
	hooks []HookFunc
}

// Register adds a hook function to the registry. It will be called for every
// install/purge event.
func (r *HookRegistry) Register(fn HookFunc) {
	r.hooks = append(r.hooks, fn)
}

// Fire calls all registered hooks with the given service ID and event.
func (r *HookRegistry) Fire(id string, event ServiceEvent) {
	for _, fn := range r.hooks {
		fn(id, event)
	}
}

// NewHookRegistry creates an empty HookRegistry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{}
}

// NewRegistry builds the full installer map, injecting dependencies into
// installers that need them. It also wires up the WhoDB hook so that
// WhoDB's config.env is regenerated whenever postgres, mysql, or redis
// is installed or purged.
func NewRegistry(siteManager *sites.Manager, queries *dbq.Queries, supervisor *services.Supervisor, siteUser, serverRoot, siteHome string) (map[string]Installer, *HookRegistry) {
	hooks := NewHookRegistry()

	m := make(map[string]Installer)
	m["postgres"] = &PostgresInstaller{
		supervisor: supervisor,
		serverRoot: serverRoot,
		siteUser:   siteUser,
	}
	m["caddy"] = NewCaddyInstaller(supervisor, serverRoot)
	m["reverb"] = &ReverbInstaller{
		siteManager: siteManager,
		queries:     queries,
		supervisor:  supervisor,
		siteUser:    siteUser,
		siteHome:    siteHome,
		serverRoot:  serverRoot,
	}
	m["meilisearch"] = &MeilisearchInstaller{
		siteManager: siteManager,
		supervisor:  supervisor,
		serverRoot:  serverRoot,
		siteUser:    siteUser,
	}
	m["typesense"] = &TypesenseInstaller{
		siteManager: siteManager,
		supervisor:  supervisor,
		serverRoot:  serverRoot,
		siteUser:    siteUser,
	}
	m["redis"] = &ValkeyInstaller{
		supervisor: supervisor,
		serverRoot: serverRoot,
		siteUser:   siteUser,
	}
	m["mailpit"] = &MailpitInstaller{
		supervisor: supervisor,
		serverRoot: serverRoot,
		siteUser:   siteUser,
	}
	m["mysql"] = &MySQLInstaller{
		supervisor: supervisor,
		serverRoot: serverRoot,
		siteUser:   siteUser,
	}
	whodb := &WhoDBInstaller{
		siteManager: siteManager,
		supervisor:  supervisor,
		serverRoot:  serverRoot,
		siteUser:    siteUser,
		queries:     queries,
		hooks:       hooks,
	}
	m["whodb"] = whodb
	m["dns"] = &DNSInstaller{}

	// Register a hook: when postgres, mysql, or redis is installed/purged,
	// regenerate WhoDB's config.env so pre-configured connections stay in sync.
	hooks.Register(func(id string, _ ServiceEvent) {
		switch id {
		case "postgres", "mysql", "redis":
			if err := whodb.RegenerateConfig(context.Background()); err != nil {
				// Non-fatal: WhoDB may not be installed yet.
				_ = err
			}
		}
	})

	return m, hooks
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// aptGet runs apt-get with DEBIAN_FRONTEND=noninteractive and the given args.
func aptGet(ctx context.Context, args ...string) error {
	return aptGetW(ctx, io.Discard, args...)
}

// aptGetW is like aptGet but writes stdout+stderr to w in real-time.
func aptGetW(ctx context.Context, w io.Writer, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, aptTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "apt-get", args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, w)
	cmd.Stdout = mw
	cmd.Stderr = mw
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("apt-get %v: %w\n%s", args, err, buf.String())
	}
	return nil
}

// aptInstall runs apt-get install -y --no-install-recommends <pkgs>.
func aptInstall(ctx context.Context, pkgs ...string) error {
	args := append([]string{"install", "-y", "--no-install-recommends"}, pkgs...)
	return aptGet(ctx, args...)
}

// aptInstallW is like aptInstall but streams output to w.
func aptInstallW(ctx context.Context, w io.Writer, pkgs ...string) error {
	args := append([]string{"install", "-y", "--no-install-recommends"}, pkgs...)
	return aptGetW(ctx, w, args...)
}

// aptPurge runs apt-get purge -y <pkgs> then autoremove.
func aptPurge(ctx context.Context, pkgs ...string) error {
	return aptPurgeW(ctx, io.Discard, pkgs...)
}

// aptPurgeW is like aptPurge but streams output to w.
func aptPurgeW(ctx context.Context, w io.Writer, pkgs ...string) error {
	args := append([]string{"purge", "-y"}, pkgs...)
	if err := aptGetW(ctx, w, args...); err != nil {
		return err
	}
	return aptGetW(ctx, w, "autoremove", "-y")
}

// aptUpdate runs apt-get update.
func aptUpdate(ctx context.Context) error {
	return aptGet(ctx, "update")
}

// aptUpdateW is like aptUpdate but streams output to w.
func aptUpdateW(ctx context.Context, w io.Writer) error {
	return aptGetW(ctx, w, "update")
}

// systemctl wraps systemctl <action> <unit>.
func systemctl(ctx context.Context, action, unit string) error {
	return systemctlW(ctx, io.Discard, action, unit)
}

// systemctlW is like systemctl but writes output to w.
func systemctlW(ctx context.Context, w io.Writer, action, unit string) error {
	cmd := exec.CommandContext(ctx, "systemctl", action, unit)
	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, w)
	cmd.Stdout = mw
	cmd.Stderr = mw
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s %s: %w: %s", action, unit, err, buf.String())
	}
	return nil
}

// enableAndStart enables and starts a systemd unit (best-effort start).
func enableAndStart(ctx context.Context, unit string) error {
	return enableAndStartW(ctx, io.Discard, unit)
}

// enableAndStartW is like enableAndStart but streams output to w.
func enableAndStartW(ctx context.Context, w io.Writer, unit string) error {
	if err := systemctlW(ctx, w, "enable", unit); err != nil {
		return err
	}
	return systemctlW(ctx, w, "start", unit)
}

// stopAndDisable stops and disables a systemd unit, ignoring errors (unit may
// not be installed).
func stopAndDisable(ctx context.Context, unit string) {
	stopAndDisableW(ctx, io.Discard, unit)
}

// stopAndDisableW is like stopAndDisable but streams output to w.
func stopAndDisableW(ctx context.Context, w io.Writer, unit string) {
	_ = systemctlW(ctx, w, "stop", unit)
	_ = systemctlW(ctx, w, "disable", unit)
}

// curlPipe runs: curl -fsSL <url> | <cmd> [args...].
// Used to pipe GPG keys into gpg --dearmor.
func curlPipe(ctx context.Context, url string, cmd string, args ...string) error {
	return curlPipeW(ctx, io.Discard, url, cmd, args...)
}

// curlPipeW is like curlPipe but streams output to w.
func curlPipeW(ctx context.Context, w io.Writer, url string, cmd string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, curlTimeout)
	defer cancel()

	curl := exec.CommandContext(ctx, "curl", "-fsSL", url)
	gpg := exec.CommandContext(ctx, cmd, args...)

	pipe, err := curl.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}
	gpg.Stdin = pipe

	var gpgBuf bytes.Buffer
	mw := io.MultiWriter(&gpgBuf, w)
	gpg.Stdout = mw
	gpg.Stderr = mw

	if err := curl.Start(); err != nil {
		return fmt.Errorf("curl start: %w", err)
	}
	if err := gpg.Start(); err != nil {
		return fmt.Errorf("gpg start: %w", err)
	}
	if err := curl.Wait(); err != nil {
		return fmt.Errorf("curl: %w", err)
	}
	if err := gpg.Wait(); err != nil {
		return fmt.Errorf("gpg: %w: %s", err, gpgBuf.String())
	}
	return nil
}

// curlDownload fetches url and writes it to dest.
func curlDownload(ctx context.Context, url, dest string) error {
	return curlDownloadW(ctx, io.Discard, url, dest)
}

// curlDownloadW is like curlDownload but streams output to w.
func curlDownloadW(ctx context.Context, w io.Writer, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, curlTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "curl", "-fsSL", "-o", dest, url)
	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, w)
	cmd.Stdout = mw
	cmd.Stderr = mw
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("curl download %s: %w\n%s", url, err, buf.String())
	}
	return nil
}

// writeFile writes content to path with given permissions.
func writeFile(path, content string, perm os.FileMode) error {
	return os.WriteFile(path, []byte(content), perm)
}

// removeFiles removes files, ignoring "not found" errors.
func removeFiles(paths ...string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// chmodFile changes file permissions.
func chmodFile(path string, perm os.FileMode) error {
	return os.Chmod(path, perm)
}

// fileExists returns true if the given absolute path exists on the filesystem.
// Use this instead of exec.LookPath to avoid false positives from the inherited
// Windows PATH in WSL2 environments.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runShell runs a shell command string (sh -c).
func runShell(ctx context.Context, command string) (string, error) {
	return runShellW(ctx, io.Discard, command)
}

// runShellW is like runShell but also streams output to w.
func runShellW(ctx context.Context, w io.Writer, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, w)
	cmd.Stdout = mw
	cmd.Stderr = mw
	err := cmd.Run()
	return buf.String(), err
}

// lsbReleaseName returns the Ubuntu/Debian codename (e.g. "noble").
func lsbReleaseName(ctx context.Context) (string, error) {
	out, err := runShell(ctx, "lsb_release -cs")
	if err != nil {
		return "", fmt.Errorf("lsb_release: %w", err)
	}
	name := string(bytes.TrimSpace([]byte(out)))
	if name == "" {
		return "", fmt.Errorf("empty lsb_release output")
	}
	return name, nil
}

// LinkIntoBinDir creates (or replaces) a symlink named `name` inside binDir
// pointing at targetBinary. The bin directory is created if absent. The
// operation is idempotent — any existing file or symlink at the destination is
// removed first.
func LinkIntoBinDir(binDir, name, targetBinary string) error {
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("bin dir: mkdir: %w", err)
	}
	dest := filepath.Join(binDir, name)
	_ = os.Remove(dest) // remove stale symlink or file
	if err := os.Symlink(targetBinary, dest); err != nil {
		return fmt.Errorf("bin dir: symlink %s → %s: %w", name, targetBinary, err)
	}
	return nil
}

// UnlinkFromBinDir removes the named symlink from binDir. A missing file is
// not treated as an error.
func UnlinkFromBinDir(binDir, name string) {
	_ = os.Remove(filepath.Join(binDir, name))
}

// removeAllExcept removes all entries in dir except those whose base name
// is listed in keep. The dir itself is not removed.
func removeAllExcept(dir string, keep ...string) error {
	keepSet := make(map[string]struct{}, len(keep))
	for _, k := range keep {
		keepSet[k] = struct{}{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if _, skip := keepSet[e.Name()]; skip {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Version-check helpers
// ---------------------------------------------------------------------------

// fetchGitHubLatestVersion queries the GitHub Releases API and returns the
// tag_name of the latest release for the given owner/repo (e.g. "caddyserver/caddy").
// The returned string includes any "v" prefix present in the tag (e.g. "v2.10.0").
func fetchGitHubLatestVersion(ctx context.Context, ownerRepo string) (string, error) {
	url := "https://api.github.com/repos/" + ownerRepo + "/releases/latest"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("github version check %s: %w", ownerRepo, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "devctl/1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github version check %s: %w", ownerRepo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github version check %s: HTTP %d", ownerRepo, resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("github version check %s: decode: %w", ownerRepo, err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("github version check %s: empty tag_name", ownerRepo)
	}
	return payload.TagName, nil
}

// fetchPackagistLatestVersion queries the Packagist API and returns the latest
// stable release version string for the given package (e.g. "laravel/reverb").
func fetchPackagistLatestVersion(ctx context.Context, pkg string) (string, error) {
	url := "https://repo.packagist.org/p2/" + pkg + ".json"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("packagist version check %s: %w", pkg, err)
	}
	req.Header.Set("User-Agent", "devctl/1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("packagist version check %s: %w", pkg, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("packagist version check %s: HTTP %d", pkg, resp.StatusCode)
	}

	// Packagist p2 format: {"packages":{"laravel/reverb":[{version:...},{...}]}}
	// Versions are sorted newest-first. We want the first stable (non-dev) version.
	var payload struct {
		Packages map[string][]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("packagist version check %s: decode: %w", pkg, err)
	}
	versions, ok := payload.Packages[pkg]
	if !ok || len(versions) == 0 {
		return "", fmt.Errorf("packagist version check %s: no versions found", pkg)
	}
	for _, v := range versions {
		ver := v.Version
		// Skip dev/alpha/beta/RC releases.
		lower := strings.ToLower(ver)
		if strings.Contains(lower, "dev") || strings.Contains(lower, "alpha") ||
			strings.Contains(lower, "beta") || strings.Contains(lower, "rc") {
			continue
		}
		return ver, nil
	}
	return versions[0].Version, nil
}

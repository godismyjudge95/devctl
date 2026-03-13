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
	"fmt"
	"io"
	"os"
	"os/exec"
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
	PurgeW(ctx context.Context, w io.Writer) error
}

// Registry maps service IDs to their Installer.
// Deprecated: use NewRegistry for installers that require dependencies.
var Registry = map[string]Installer{
	"postgres": &PostgresInstaller{},
}

// NewRegistry builds the full installer map, injecting dependencies into
// installers that need them (e.g. ReverbInstaller, MeilisearchInstaller).
func NewRegistry(siteManager *sites.Manager, queries *dbq.Queries, supervisor *services.Supervisor, siteUser, siteHome string) map[string]Installer {
	m := make(map[string]Installer, len(Registry)+2)
	for k, v := range Registry {
		m[k] = v
	}
	m["caddy"] = NewCaddyInstaller(supervisor, siteHome)
	m["reverb"] = &ReverbInstaller{
		siteManager: siteManager,
		queries:     queries,
		supervisor:  supervisor,
		siteUser:    siteUser,
		siteHome:    siteHome,
	}
	m["meilisearch"] = &MeilisearchInstaller{
		siteManager: siteManager,
		supervisor:  supervisor,
		siteHome:    siteHome,
	}
	m["typesense"] = &TypesenseInstaller{
		siteManager: siteManager,
		supervisor:  supervisor,
		siteHome:    siteHome,
	}
	m["redis"] = &ValkeyInstaller{
		supervisor: supervisor,
		siteHome:   siteHome,
	}
	m["mailpit"] = &MailpitInstaller{
		supervisor: supervisor,
		siteHome:   siteHome,
	}
	m["mysql"] = &MySQLInstaller{
		supervisor: supervisor,
		siteHome:   siteHome,
	}
	return m
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

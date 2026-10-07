// Package paths is the single source of truth for every filesystem path that
// devctl owns. All other packages must import this package instead of
// constructing paths inline.
//
// All functions accept serverRoot — the absolute path to the server directory
// (e.g. "/home/alice/ddev/sites/server"). This is stored in the systemd unit
// as DEVCTL_SERVER_ROOT and loaded by config.Load().
//
// Paths that devctl does NOT own (e.g. /run/php/, /etc/systemd/,
// /etc/resolv.conf) are intentionally absent — those are managed by the OS or
// other tools.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ServerDir returns the root directory for all devctl-managed service data.
// For historical reasons this accepts serverRoot directly — it is a no-op that
// exists so callers have a consistent API.
//
//	serverRoot  (e.g. /home/alice/ddev/sites/server)
func ServerDir(serverRoot string) string {
	return serverRoot
}

// DevctlDir returns the directory used for devctl's own runtime state
// (database, prepend.php, binary).
//
//	{serverRoot}/devctl
func DevctlDir(serverRoot string) string {
	return filepath.Join(serverRoot, "devctl")
}

// DBPath returns the absolute path to the devctl SQLite database.
//
//	{serverRoot}/devctl/devctl.db
func DBPath(serverRoot string) string {
	return filepath.Join(DevctlDir(serverRoot), "devctl.db")
}

// PrependPath returns the absolute path to the PHP auto-prepend file.
//
//	{serverRoot}/devctl/prepend.php
func PrependPath(serverRoot string) string {
	return filepath.Join(DevctlDir(serverRoot), "prepend.php")
}

// BinaryPath returns the absolute path to the installed devctl binary.
//
//	{serverRoot}/devctl/devctl
func BinaryPath(serverRoot string) string {
	return filepath.Join(DevctlDir(serverRoot), "devctl")
}

// ElevateSocket returns the unix socket for the elevated bind supervisor.
//
//	{serverRoot}/devctl/elevate.sock
func ElevateSocket(serverRoot string) string {
	return filepath.Join(DevctlDir(serverRoot), "elevate.sock")
}

// ServiceDir returns the data directory for a managed service.
//
//	{serverRoot}/<id>
func ServiceDir(serverRoot, id string) string {
	return filepath.Join(serverRoot, id)
}

// BinDir returns the shared symlink farm that is prepended to PATH via the
// user's shell config files (e.g. ~/.zshenv for zsh users, ~/.bashrc for bash
// users). Each service installer drops a symlink here on install and removes
// it on purge.
//
//	{serverRoot}/bin
func BinDir(serverRoot string) string {
	return filepath.Join(serverRoot, "bin")
}

// LogsDir returns the directory where all service log files are written.
// Every service writes its log to {logsDir}/{serviceID}.log.
//
//	{serverRoot}/logs
func LogsDir(serverRoot string) string {
	return filepath.Join(serverRoot, "logs")
}

// LogPath returns the absolute path to the log file for the given service.
//
//	{serverRoot}/logs/{id}.log
func LogPath(serverRoot, id string) string {
	return filepath.Join(LogsDir(serverRoot), id+".log")
}

// ExpandHome expands a leading ~ or $HOME in p. Other values are returned
// unchanged (after TrimSpace). Empty input stays empty.
func ExpandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	switch {
	case p == "~" || p == "$HOME":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "$HOME/"):
		return filepath.Join(home, strings.TrimPrefix(p, "$HOME/"))
	default:
		return p
	}
}

// DefaultSitesDir is the install-time sites directory for this OS.
// Linux: {siteHome}/ddev/sites. Darwin: {siteHome}/Code/sites.
func DefaultSitesDir(siteHome string) string {
	return DefaultSitesDirFor(runtime.GOOS, siteHome)
}

// DefaultSitesDirFor is DefaultSitesDir for a given GOOS.
func DefaultSitesDirFor(goos, siteHome string) string {
	if goos == "darwin" {
		return filepath.Join(siteHome, "Code", "sites")
	}
	return filepath.Join(siteHome, "ddev", "sites")
}

// DefaultServerRoot is DefaultSitesDir(siteHome)/server.
func DefaultServerRoot(siteHome string) string {
	return filepath.Join(DefaultSitesDir(siteHome), "server")
}

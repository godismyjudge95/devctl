// Package paths is the single source of truth for every filesystem path that
// devctl owns. All other packages must import this package instead of
// constructing paths inline.
//
// Paths that devctl does NOT own (e.g. /run/php/, /etc/systemd/,
// /etc/resolv.conf) are intentionally absent — those are managed by the OS or
// other tools.
package paths

import "path/filepath"

// ServerDir returns the root directory for all devctl-managed service data.
//
//	~/sites/server
func ServerDir(siteHome string) string {
	return filepath.Join(siteHome, "sites", "server")
}

// DevctlDir returns the directory used for devctl's own runtime state
// (database, prepend.php, binary).
//
//	~/sites/server/devctl
func DevctlDir(siteHome string) string {
	return filepath.Join(ServerDir(siteHome), "devctl")
}

// DBPath returns the absolute path to the devctl SQLite database.
//
//	~/sites/server/devctl/devctl.db
func DBPath(siteHome string) string {
	return filepath.Join(DevctlDir(siteHome), "devctl.db")
}

// PrependPath returns the absolute path to the PHP auto-prepend file.
//
//	~/sites/server/devctl/prepend.php
func PrependPath(siteHome string) string {
	return filepath.Join(DevctlDir(siteHome), "prepend.php")
}

// BinaryPath returns the absolute path to the installed devctl binary.
//
//	~/sites/server/devctl/devctl
func BinaryPath(siteHome string) string {
	return filepath.Join(DevctlDir(siteHome), "devctl")
}

// ServiceDir returns the data directory for a managed service.
//
//	~/sites/server/<id>
func ServiceDir(siteHome, id string) string {
	return filepath.Join(ServerDir(siteHome), id)
}

// BinDir returns the shared symlink farm that is prepended to PATH via
// /etc/profile.d/devctl.sh. Each service installer drops a symlink here on
// install and removes it on purge.
//
//	~/sites/server/bin
func BinDir(siteHome string) string {
	return filepath.Join(ServerDir(siteHome), "bin")
}

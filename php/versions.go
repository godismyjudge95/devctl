package php

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/danielgormly/devctl/paths"
)

// Version represents an installed PHP-FPM version.
type Version struct {
	Version   string `json:"version"`    // e.g. "8.3"
	FPMSocket string `json:"fpm_socket"` // unix socket path
	Status    string `json:"status"`     // "running" | "stopped" | "unknown"
}

// FPMServiceID returns the canonical service registry ID for a PHP-FPM version.
func FPMServiceID(ver string) string {
	return "php-fpm-" + ver
}

// PHPDir returns the directory where a PHP version's binaries and config live.
// e.g. /home/alice/sites/server/php/8.3
func PHPDir(ver, siteHome string) string {
	return filepath.Join(paths.ServerDir(siteHome), "php", ver)
}

// FPMBinary returns the path to the php-fpm binary for the given version.
func FPMBinary(ver, siteHome string) string {
	return filepath.Join(PHPDir(ver, siteHome), "php-fpm")
}

// FPMSocket returns the conventional unix socket path for the given version.
// Matches the ondrej/php PPA convention so Caddy/Nginx configs work without changes.
func FPMSocket(ver string) string {
	return fmt.Sprintf("/run/php/php%s-fpm.sock", ver)
}

// FPMConfigPath returns the path to the php-fpm.conf for the given version.
func FPMConfigPath(ver, siteHome string) string {
	return filepath.Join(PHPDir(ver, siteHome), "php-fpm.conf")
}

// PHPIniPath returns the path to the php.ini for the given version.
func PHPIniPath(ver, siteHome string) string {
	return filepath.Join(PHPDir(ver, siteHome), "php.ini")
}

// FPMLogPath returns the path to the php-fpm log file for the given version.
func FPMLogPath(ver, siteHome string) string {
	return filepath.Join(PHPDir(ver, siteHome), "php-fpm-www.log")
}

var versionRe = regexp.MustCompile(`^(\d+\.\d+)$`)

// InstalledVersions scans {siteHome}/sites/server/php/ for installed PHP versions.
// A version is considered installed if its php-fpm binary exists.
// Returns them sorted newest-first.
func InstalledVersions(siteHome string) ([]Version, error) {
	phpBase := filepath.Join(paths.ServerDir(siteHome), "php")
	entries, err := os.ReadDir(phpBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", phpBase, err)
	}

	var versions []Version
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ver := e.Name()
		if !versionRe.MatchString(ver) {
			continue
		}
		// Only include if php-fpm binary is present.
		if _, err := os.Stat(FPMBinary(ver, siteHome)); os.IsNotExist(err) {
			continue
		}
		versions = append(versions, Version{
			Version:   ver,
			FPMSocket: FPMSocket(ver),
		})
	}

	// Sort newest first.
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version > versions[j].Version
	})

	return versions, nil
}

// UpdateGlobalSymlink points ~/sites/server/bin/php at the CLI binary for the
// highest installed PHP version. If no versions are installed the symlink is
// removed. Errors are non-fatal — callers should log but continue.
func UpdateGlobalSymlink(siteHome string) error {
	globalLink := filepath.Join(paths.BinDir(siteHome), "php")

	versions, err := InstalledVersions(siteHome)
	if err != nil {
		return fmt.Errorf("update global php symlink: %w", err)
	}

	// Remove any existing symlink or file.
	_ = os.Remove(globalLink)

	if len(versions) == 0 {
		return nil
	}

	// Versions are sorted newest-first; use the first one.
	best := versions[0].Version
	cliBin := filepath.Join(PHPDir(best, siteHome), "php")
	if err := os.Symlink(cliBin, globalLink); err != nil {
		return fmt.Errorf("update global php symlink: %w", err)
	}
	return nil
}

package php

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Version represents an installed PHP-FPM version.
type Version struct {
	Version    string   `json:"version"`    // e.g. "8.3"
	FPMSocket  string   `json:"fpm_socket"` // e.g. "/run/php/php8.3-fpm.sock"
	Extensions []string `json:"extensions"` // enabled extension names
}

var versionRe = regexp.MustCompile(`^(\d+\.\d+)$`)

// InstalledVersions scans /etc/php/ for installed PHP-FPM versions.
// It returns them sorted newest-first.
func InstalledVersions() ([]Version, error) {
	entries, err := os.ReadDir("/etc/php")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read /etc/php: %w", err)
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
		// Only include if php-fpm is installed for this version.
		fpmConf := filepath.Join("/etc/php", ver, "fpm")
		if _, err := os.Stat(fpmConf); os.IsNotExist(err) {
			continue
		}
		exts, _ := EnabledExtensions(ver)
		versions = append(versions, Version{
			Version:    ver,
			FPMSocket:  fmt.Sprintf("/run/php/php%s-fpm.sock", ver),
			Extensions: exts,
		})
	}

	// Sort newest first.
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version > versions[j].Version
	})

	return versions, nil
}

// EnabledExtensions returns the names of extensions that are enabled
// (have .ini files in /etc/php/{ver}/fpm/conf.d/).
func EnabledExtensions(ver string) ([]string, error) {
	confDir := filepath.Join("/etc/php", ver, "fpm", "conf.d")
	entries, err := os.ReadDir(confDir)
	if err != nil {
		return nil, err
	}

	extRe := regexp.MustCompile(`^\d+-(.+)\.ini$`)
	var exts []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := extRe.FindStringSubmatch(e.Name())
		if len(m) == 2 {
			exts = append(exts, m[1])
		}
	}
	sort.Strings(exts)
	return exts, nil
}

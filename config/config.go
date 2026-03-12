package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// Config is the top-level application configuration.
// Runtime settings (ports, etc.) live in the SQLite settings table and are
// managed via the API. Service definitions are static in defaults.go.
type Config struct {
	// DBPath is the absolute path to the SQLite database.
	DBPath string
	// SiteUser is the non-root OS user who owns ~/sites (e.g. "daniel").
	// Set via the DEVCTL_SITE_USER environment variable.
	SiteUser string
	// SiteHome is the home directory of SiteUser (e.g. "/home/alice").
	SiteHome string
}

// Load creates the devctl config directory if needed and returns a Config.
func Load() (*Config, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}

	siteUser, siteHome, err := resolveSiteUser()
	if err != nil {
		return nil, err
	}

	return &Config{
		DBPath:   filepath.Join(dir, "devctl.db"),
		SiteUser: siteUser,
		SiteHome: siteHome,
	}, nil
}

// resolveSiteUser returns the non-root user and their home directory.
// It reads DEVCTL_SITE_USER; if unset it falls back to SUDO_USER.
func resolveSiteUser() (string, string, error) {
	name := os.Getenv("DEVCTL_SITE_USER")
	if name == "" {
		name = os.Getenv("SUDO_USER")
	}
	if name == "" {
		return "", "", fmt.Errorf(
			"DEVCTL_SITE_USER is not set — add 'Environment=DEVCTL_SITE_USER=<your-username>' to devctl.service",
		)
	}
	u, err := user.Lookup(name)
	if err != nil {
		return "", "", fmt.Errorf("DEVCTL_SITE_USER %q: %w", name, err)
	}
	return u.Username, u.HomeDir, nil
}

func configDir() (string, error) {
	// devctl runs as a systemd system service (root). Use a system-wide path
	// so the config is stable regardless of which user account launched the
	// process.
	dir := "/etc/devctl"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

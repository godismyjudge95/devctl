package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/danielgormly/devctl/paths"
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
	// ServerRoot is the absolute path to the devctl server directory
	// (e.g. "/home/alice/ddev/sites/server"). It is baked into the systemd
	// unit as DEVCTL_SERVER_ROOT at install time. When unset it falls back to
	// {SiteHome}/sites/server for backwards compatibility.
	ServerRoot string
}

// Load resolves the site user and returns a Config.
// The DB directory is created lazily by db.Open — no manual MkdirAll needed.
func Load() (*Config, error) {
	siteUser, siteHome, err := resolveSiteUser()
	if err != nil {
		return nil, err
	}

	serverRoot := resolveServerRoot(siteHome)

	return &Config{
		DBPath:     paths.DBPath(serverRoot),
		SiteUser:   siteUser,
		SiteHome:   siteHome,
		ServerRoot: serverRoot,
	}, nil
}

// resolveServerRoot returns the server root directory. It reads
// DEVCTL_SERVER_ROOT; if unset it falls back to {siteHome}/sites/server for
// backwards compatibility with installs that predate this setting.
func resolveServerRoot(siteHome string) string {
	if v := os.Getenv("DEVCTL_SERVER_ROOT"); v != "" {
		return filepath.Clean(v)
	}
	// Legacy fallback: derive from siteHome the same way the old paths package did.
	return filepath.Join(siteHome, "sites", "server")
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

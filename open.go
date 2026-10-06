package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/danielgormly/devctl/db"
	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/elevate"
	"github.com/danielgormly/devctl/paths"
)

const devctlServiceFile = "/etc/systemd/system/devctl.service"

// runOpen finds the site whose root_path contains the current working directory
// and opens its URL in the default browser.
func runOpen() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	serverRoot := resolveServerRootForOpen()
	dbPath := paths.DBPath(serverRoot)
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w\nhint: is devctl installed and running?", err)
	}
	defer database.Close()

	ctx := context.Background()
	queries := dbq.New(database)

	allSites, err := queries.GetAllSites(ctx)
	if err != nil {
		return fmt.Errorf("query sites: %w", err)
	}

	// Walk up the directory tree from CWD until we find a matching root_path.
	dir := cwd
	for {
		for _, site := range allSites {
			if site.RootPath == dir {
				scheme := "https"
				if site.Https == 0 {
					scheme = "http"
				}
				url := scheme + "://" + site.Domain
				fmt.Println(url)
				openCmd := "xdg-open"
				if runtime.GOOS == "darwin" {
					openCmd = "open"
				}
				cmd := exec.Command(openCmd, url)
				if err := cmd.Start(); err != nil {
					return fmt.Errorf("xdg-open: %w", err)
				}
				return nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root without a match
		}
		dir = parent
	}

	return fmt.Errorf("no site found for %q\nhint: run 'devctl' to start the daemon and auto-discover sites", cwd)
}

// resolveServerRootForOpen reads DEVCTL_SERVER_ROOT from the environment,
// then from the parent unit file (LaunchAgent on Darwin, systemd on Linux).
// Falls back to {HOME}/sites/server for legacy installs.
func resolveServerRootForOpen() string {
	if v := os.Getenv("DEVCTL_SERVER_ROOT"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	candidates := []string{devctlServiceFile}
	if runtime.GOOS == "darwin" {
		candidates = []string{elevate.LaunchAgentPath(home), devctlServiceFile}
	}
	for _, f := range candidates {
		if v := parseServerRootFromUnit(f); v != "" {
			return v
		}
	}
	return filepath.Join(home, "sites", "server")
}

func parseServerRootFromUnit(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := regexp.MustCompile(`<key>DEVCTL_SERVER_ROOT</key>\s*<string>([^<]+)</string>`).FindStringSubmatch(string(data)); len(m) == 2 {
		return m[1]
	}
	prefix := "Environment=DEVCTL_SERVER_ROOT="
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			if v := strings.TrimPrefix(line, prefix); v != "" {
				return v
			}
		}
	}
	return ""
}

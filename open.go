package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/danielgormly/devctl/db"
	dbq "github.com/danielgormly/devctl/db/queries"
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
				cmd := exec.Command("xdg-open", url)
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

// resolveServerRootForOpen reads DEVCTL_SERVER_ROOT from the systemd service
// file. Falls back to {HOME}/sites/server for legacy installs.
func resolveServerRootForOpen() string {
	data, err := os.ReadFile(devctlServiceFile)
	if err == nil {
		prefix := "Environment=DEVCTL_SERVER_ROOT="
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, prefix) {
				if v := strings.TrimPrefix(line, prefix); v != "" {
					return v
				}
			}
		}
	}
	// Legacy fallback.
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "sites", "server")
}

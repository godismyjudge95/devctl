package install

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/sites"
)

// CleanupRemovedWhoDB deletes leftover WhoDB files, the bin symlink, log,
// and the whodb.test Caddy site from installs that predate the built-in
// Databases explorer.
func CleanupRemovedWhoDB(ctx context.Context, siteManager *sites.Manager, serverRoot string) {
	if serverRoot == "" {
		return
	}
	dir := paths.ServiceDir(serverRoot, "whodb")
	link := filepath.Join(paths.BinDir(serverRoot), "whodb")
	logPath := paths.LogPath(serverRoot, "whodb")

	_, dirErr := os.Stat(dir)
	_, linkErr := os.Lstat(link)
	_, logErr := os.Stat(logPath)
	if dirErr != nil && linkErr != nil && logErr != nil && siteManager == nil {
		return
	}

	UnlinkFromBinDir(paths.BinDir(serverRoot), "whodb")
	if siteManager != nil {
		if err := siteManager.Delete(ctx, "whodb-test"); err != nil {
			_ = err
		}
	}
	if dirErr == nil {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("cleanup: remove leftover WhoDB dir: %v", err)
		}
	}
	if logErr == nil {
		_ = os.Remove(logPath)
	}
	if dirErr == nil || linkErr == nil || logErr == nil {
		log.Printf("cleanup: removed leftover WhoDB install")
	}
}

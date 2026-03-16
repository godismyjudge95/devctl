package install

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

const (
	meilisearchVersion = "v1.37.0"
	meilisearchURL     = "https://github.com/meilisearch/meilisearch/releases/download/" + meilisearchVersion + "/meilisearch-linux-amd64"
)

// MeilisearchInstaller downloads the Meilisearch binary to
// {serverRoot}/meilisearch/, generates a master key, writes
// config.env, and registers a Caddy reverse-proxy vhost at meilisearch.test.
type MeilisearchInstaller struct {
	siteManager *sites.Manager
	supervisor  *services.Supervisor
	serverRoot  string // absolute path to the devctl server directory
}

func (m *MeilisearchInstaller) ServiceID() string { return "meilisearch" }

func (m *MeilisearchInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(m.serverRoot, "meilisearch"), "meilisearch"))
}

func (m *MeilisearchInstaller) Install(ctx context.Context) error {
	return m.InstallW(ctx, io.Discard)
}

func (m *MeilisearchInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if m.IsInstalled() {
		fmt.Fprintln(w, "meilisearch: already installed")
		return nil
	}

	meiliDir := paths.ServiceDir(m.serverRoot, "meilisearch")
	binPath := filepath.Join(meiliDir, "meilisearch")
	envPath := filepath.Join(meiliDir, "config.env")

	// 1. Create directory.
	fmt.Fprintln(w, "meilisearch: creating directory...")
	if err := os.MkdirAll(meiliDir, 0755); err != nil {
		return fmt.Errorf("meilisearch: create dir: %w", err)
	}

	// 2. Download binary.
	fmt.Fprintf(w, "meilisearch: downloading %s...\n", meilisearchVersion)
	if err := curlDownloadW(ctx, w, meilisearchURL, binPath); err != nil {
		return fmt.Errorf("meilisearch: download: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("meilisearch: chmod binary: %w", err)
	}

	// 3. Symlink into the shared bin dir so meilisearch is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(m.serverRoot), "meilisearch", binPath); err != nil {
		fmt.Fprintf(w, "meilisearch: warning: %v\n", err)
	}

	// 4. Generate a random 32-byte hex master key.
	key, err := generateRandomHex(32)
	if err != nil {
		return fmt.Errorf("meilisearch: generate master key: %w", err)
	}

	// 4. Write config.env.
	fmt.Fprintln(w, "meilisearch: writing config.env...")
	envContent := fmt.Sprintf("MEILI_MASTER_KEY=%s\nMEILI_HOST=https://meilisearch.test\n", key)
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("meilisearch: write config.env: %w", err)
	}

	// 5. Register Caddy reverse-proxy vhost at meilisearch.test.
	fmt.Fprintln(w, "meilisearch: creating meilisearch.test Caddy vhost...")
	_, err = m.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:     "meilisearch.test",
		SiteType:   "ws", // reverse_proxy handler — works for plain HTTP too
		WSUpstream: "127.0.0.1:7700",
		HTTPS:      true,
	})
	if err != nil {
		// Best-effort: vhost may already exist.
		fmt.Fprintf(w, "meilisearch: warning: create site: %v\n", err)
	}

	fmt.Fprintln(w, "meilisearch: install complete")
	return nil
}

func (m *MeilisearchInstaller) Purge(ctx context.Context) error {
	return m.PurgeW(ctx, io.Discard)
}

func (m *MeilisearchInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	// Stop the supervised process first.
	if err := m.supervisor.Stop("meilisearch"); err != nil {
		fmt.Fprintf(w, "meilisearch: warning: stop process: %v\n", err)
	}

	// Remove the Caddy vhost and DB row.
	if m.siteManager != nil {
		if err := m.siteManager.Delete(ctx, "meilisearch-test"); err != nil {
			fmt.Fprintf(w, "meilisearch: warning: delete site: %v\n", err)
		}
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(m.serverRoot), "meilisearch")

	// Remove the directory.
	meiliDir := paths.ServiceDir(m.serverRoot, "meilisearch")
	if err := os.RemoveAll(meiliDir); err != nil {
		return fmt.Errorf("meilisearch: remove dir: %w", err)
	}

	fmt.Fprintln(w, "meilisearch: purge complete")
	return nil
}

// generateRandomHex returns a random hex string of n bytes (length 2*n).
func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

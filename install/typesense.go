package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

const (
	typesenseVersion = "30.1"
	typesenseTarURL  = "https://dl.typesense.org/releases/" + typesenseVersion + "/typesense-server-" + typesenseVersion + "-linux-amd64.tar.gz"
)

// TypesenseInstaller downloads the Typesense binary to
// {serverRoot}/typesense/, generates an API key, writes config.env,
// and registers a Caddy reverse-proxy vhost at typesense.test.
type TypesenseInstaller struct {
	siteManager *sites.Manager
	supervisor  *services.Supervisor
	serverRoot  string // absolute path to the devctl server directory
	siteUser    string
}

func (t *TypesenseInstaller) ServiceID() string { return "typesense" }

func (t *TypesenseInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(t.serverRoot, "typesense"), "typesense-server"))
}

func (t *TypesenseInstaller) Install(ctx context.Context) error {
	return t.InstallW(ctx, io.Discard)
}

func (t *TypesenseInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if t.IsInstalled() {
		fmt.Fprintln(w, "typesense: already installed")
		return nil
	}

	tsDir := paths.ServiceDir(t.serverRoot, "typesense")
	binPath := filepath.Join(tsDir, "typesense-server")
	envPath := filepath.Join(tsDir, "config.env")
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("typesense-%s.tar.gz", typesenseVersion))
	defer os.Remove(tmpTar)

	// 1. Create directories.
	fmt.Fprintln(w, "typesense: creating directory...")
	if err := os.MkdirAll(tsDir, 0755); err != nil {
		return fmt.Errorf("typesense: create dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(tsDir, "data"), 0755); err != nil {
		return fmt.Errorf("typesense: create data dir: %w", err)
	}

	// 2. Download tarball.
	fmt.Fprintf(w, "typesense: downloading %s...\n", typesenseVersion)
	if err := curlDownloadW(ctx, w, typesenseTarURL, tmpTar); err != nil {
		return fmt.Errorf("typesense: download: %w", err)
	}

	// 3. Extract binary from tarball.
	fmt.Fprintln(w, "typesense: extracting binary...")
	if err := extractFromTar(tmpTar, "typesense-server", binPath); err != nil {
		return fmt.Errorf("typesense: extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("typesense: chmod binary: %w", err)
	}

	// 4. Symlink into the shared bin dir so typesense-server is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(t.serverRoot), "typesense-server", binPath); err != nil {
		fmt.Fprintf(w, "typesense: warning: %v\n", err)
	}

	// 5. Generate a random 32-byte hex API key.
	key, err := generateRandomHex(32)
	if err != nil {
		return fmt.Errorf("typesense: generate api key: %w", err)
	}

	// 5. Write config.env.
	fmt.Fprintln(w, "typesense: writing config.env...")
	envContent := fmt.Sprintf("TYPESENSE_API_KEY=%s\nTYPESENSE_HOST=https://typesense.test\n", key)
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("typesense: write config.env: %w", err)
	}

	// 6. Register Caddy reverse-proxy vhost at typesense.test.
	fmt.Fprintln(w, "typesense: creating typesense.test Caddy vhost...")
	_, err = t.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:     "typesense.test",
		SiteType:   "ws", // reverse_proxy handler — works for plain HTTP too
		WSUpstream: "127.0.0.1:8108",
		HTTPS:      true,
	})
	if err != nil {
		// Best-effort: vhost may already exist.
		fmt.Fprintf(w, "typesense: warning: create site: %v\n", err)
	}

	// 7. Transfer ownership to the site user.
	if t.siteUser != "" {
		fmt.Fprintf(w, "typesense: chowning %s to %s...\n", tsDir, t.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", t.siteUser, t.siteUser, tsDir)
		if out, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("typesense: chown: %w\n%s", err, out)
		}
	}

	fmt.Fprintln(w, "typesense: install complete")
	return nil
}

func (t *TypesenseInstaller) Purge(ctx context.Context) error {
	return t.PurgeW(ctx, io.Discard, false)
}

func (t *TypesenseInstaller) PurgeW(ctx context.Context, w io.Writer, _ bool) error {
	// Stop the supervised process first.
	if err := t.supervisor.Stop("typesense"); err != nil {
		fmt.Fprintf(w, "typesense: warning: stop process: %v\n", err)
	}

	// Remove the Caddy vhost and DB row.
	if t.siteManager != nil {
		if err := t.siteManager.Delete(ctx, "typesense-test"); err != nil {
			fmt.Fprintf(w, "typesense: warning: delete site: %v\n", err)
		}
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(t.serverRoot), "typesense-server")

	// Remove the directory.
	tsDir := paths.ServiceDir(t.serverRoot, "typesense")
	if err := os.RemoveAll(tsDir); err != nil {
		return fmt.Errorf("typesense: remove dir: %w", err)
	}

	fmt.Fprintln(w, "typesense: purge complete")
	return nil
}

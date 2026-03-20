package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

	// 6. Write config.env with Laravel/app connection info.
	fmt.Fprintln(w, "typesense: writing config.env...")
	envContent := fmt.Sprintf("TYPESENSE_API_KEY=%s\nTYPESENSE_HOST=https://typesense.test\n", key)
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("typesense: write config.env: %w", err)
	}

	// 7. Write typesense.ini with full defaults.
	fmt.Fprintln(w, "typesense: writing typesense.ini...")
	if err := writeTypesenseConf(tsDir, key); err != nil {
		return fmt.Errorf("typesense: write typesense.ini: %w", err)
	}

	// 8. Register Caddy reverse-proxy vhost at typesense.test.
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

	// 9. Transfer ownership to the site user.
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

// LatestVersion queries GitHub Releases for the latest Typesense version.
func (t *TypesenseInstaller) LatestVersion(ctx context.Context) (string, error) {
	tag, err := fetchGitHubLatestVersion(ctx, "typesense/typesense")
	if err != nil {
		return "", err
	}
	// Typesense tags may be prefixed with "v" (e.g. "v30.1"); strip it to
	// match the version format used in download URLs ("30.1").
	return strings.TrimPrefix(tag, "v"), nil
}

// UpdateW stops Typesense and replaces the binary with the latest version.
// The caller (API handler) is responsible for restarting the service.
func (t *TypesenseInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := t.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("typesense: update: %w", err)
	}
	dlURL := fmt.Sprintf("https://dl.typesense.org/releases/%s/typesense-server-%s-linux-amd64.tar.gz", latest, latest)

	tsDir := paths.ServiceDir(t.serverRoot, "typesense")
	binPath := filepath.Join(tsDir, "typesense-server")
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("typesense-%s-update.tar.gz", latest))
	defer os.Remove(tmpTar)

	fmt.Fprintf(w, "typesense: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpTar); err != nil {
		return fmt.Errorf("typesense: update download: %w", err)
	}

	fmt.Fprintln(w, "typesense: stopping typesense...")
	if err := t.supervisor.Stop("typesense"); err != nil {
		fmt.Fprintf(w, "typesense: warning: stop: %v\n", err)
	}

	fmt.Fprintln(w, "typesense: replacing binary...")
	if err := extractFromTar(tmpTar, "typesense-server", binPath); err != nil {
		return fmt.Errorf("typesense: update extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("typesense: update chmod: %w", err)
	}

	fmt.Fprintf(w, "typesense: binary replaced with %s\n", latest)
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

// EnsureTypesenseConf writes typesense.ini to the Typesense service directory
// if the file is missing. Reads the API key from the existing config.env so it
// matches the already-provisioned key. Safe to call on every startup — it is a
// no-op when typesense.ini already exists.
func EnsureTypesenseConf(serverRoot string) error {
	tsDir := paths.ServiceDir(serverRoot, "typesense")
	key := readEnvKey(filepath.Join(tsDir, "config.env"), "TYPESENSE_API_KEY")
	return writeTypesenseConf(tsDir, key)
}

// writeTypesenseConf writes a typesense.ini to dir/typesense.ini.
// Typesense has no official default config template; the INI format mirrors
// CLI flags without the leading -- (see https://typesense.org/docs/30.1/api/server-configuration.html).
// The file is only written if it does not yet exist so user edits are preserved.
func writeTypesenseConf(dir, apiKey string) error {
	confPath := filepath.Join(dir, "typesense.ini")
	if _, err := os.Stat(confPath); err == nil {
		return nil // already exists — don't overwrite
	}
	conf := fmt.Sprintf(`; devctl-managed Typesense configuration
; See https://typesense.org/docs/30.1/api/server-configuration.html
; Format: key = value  (same as CLI flags, without the leading --)

[server]

; Required settings
api-key = %s
data-dir = ./data

; Networking
api-address = 127.0.0.1
api-port = 8108

; CORS — allow browser JS clients to access Typesense directly
enable-cors = true
; cors-domains = https://example.com,https://example2.com

; Logging (logs to stdout by default; set log-dir to write to files)
; log-dir = ./logs
; enable-access-logging = false
; enable-search-logging = false
; log-slow-requests-time-ms = -1

; Resource limits (defaults shown)
; thread-pool-size = <NUM_CORES * 8>
; num-collections-parallel-load = <NUM_CORES * 4>
; num-documents-parallel-load = 1000
; cache-num-entries = 1000
; disk-used-max-percentage = 100
; memory-used-max-percentage = 100

; On-disk DB fine tuning (RocksDB)
; db-write-buffer-size = 4194304
; db-max-write-buffer-number = 2
; db-max-log-file-size = 4194304
; db-keep-log-file-num = 5
; max-indexing-concurrency = 4
`, apiKey)
	return os.WriteFile(confPath, []byte(conf), 0644)
}

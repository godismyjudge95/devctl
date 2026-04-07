package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
	"github.com/danielgormly/devctl/sites"
)

// MaxIOInstaller downloads the MaxIO binary to {serverRoot}/maxio/,
// writes config.env with default credentials, creates the data directory, and
// registers Caddy reverse-proxy vhosts at maxio.test and s3.maxio.test.
type MaxIOInstaller struct {
	siteManager *sites.Manager
	supervisor  *services.Supervisor
	serverRoot  string
	siteUser    string
}

func (m *MaxIOInstaller) ServiceID() string { return "maxio" }

func (m *MaxIOInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(m.serverRoot, "maxio"), "maxio"))
}

func (m *MaxIOInstaller) Install(ctx context.Context) error {
	return m.InstallW(ctx, io.Discard)
}

func (m *MaxIOInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if m.IsInstalled() {
		fmt.Fprintln(w, "maxio: already installed")
		return nil
	}

	maxioDir := paths.ServiceDir(m.serverRoot, "maxio")
	binPath := filepath.Join(maxioDir, "maxio")
	dataDir := filepath.Join(maxioDir, "data")
	envPath := filepath.Join(maxioDir, "config.env")

	// 1. Create directories.
	fmt.Fprintln(w, "maxio: creating directories...")
	for _, d := range []string{maxioDir, dataDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("maxio: create dir %s: %w", d, err)
		}
	}

	// 2. Resolve the latest release version and download URL.
	fmt.Fprintln(w, "maxio: fetching latest release info...")
	version, dlURL, err := maxioLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("maxio: resolve release: %w", err)
	}

	// 3. Download tar.gz archive.
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("maxio-%s-linux-amd64.tar.gz", version))
	defer os.Remove(tmpTar)

	fmt.Fprintf(w, "maxio: downloading %s...\n", version)
	if err := curlDownloadW(ctx, w, dlURL, tmpTar); err != nil {
		return fmt.Errorf("maxio: download: %w", err)
	}

	// 4. Extract the maxio binary from the tar.gz archive.
	fmt.Fprintln(w, "maxio: extracting binary...")
	if err := extractFromTar(tmpTar, "maxio", binPath); err != nil {
		return fmt.Errorf("maxio: extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("maxio: chmod binary: %w", err)
	}

	// 5. Symlink into the shared bin dir so maxio is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(m.serverRoot), "maxio", binPath); err != nil {
		fmt.Fprintf(w, "maxio: warning: %v\n", err)
	}

	// 6. Write config.env with credentials and configuration.
	fmt.Fprintln(w, "maxio: writing config.env...")
	envContent := fmt.Sprintf(
		"MAXIO_ACCESS_KEY=DEVCTL\n"+
			"MAXIO_SECRET_KEY=DEVCTL\n"+
			"MAXIO_PORT=9000\n"+
			"MAXIO_ADDRESS=127.0.0.1\n"+
			"MAXIO_DATA_DIR=%s\n"+
			"MAXIO_HOST=https://maxio.test\n"+
			"MAXIO_S3_HOST=https://s3.maxio.test\n",
		dataDir,
	)
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("maxio: write config.env: %w", err)
	}

	// 7. Write connection.env with Laravel-compatible AWS keys for easy copy-paste into .env.
	connPath := filepath.Join(maxioDir, "connection.env")
	connContent := "AWS_ACCESS_KEY_ID=DEVCTL\nAWS_SECRET_ACCESS_KEY=DEVCTL\nAWS_DEFAULT_REGION=us-east-1\nAWS_ENDPOINT=https://s3.maxio.test\nAWS_USE_PATH_STYLE_ENDPOINT=true\n"
	if err := os.WriteFile(connPath, []byte(connContent), 0600); err != nil {
		return fmt.Errorf("maxio: write connection.env: %w", err)
	}

	// 8. Register Caddy reverse-proxy vhosts.
	fmt.Fprintln(w, "maxio: creating maxio.test Caddy vhost...")
	_, err = m.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:       "maxio.test",
		SiteType:     "ws",
		WSUpstream:   "127.0.0.1:9000",
		HTTPS:        true,
		ServiceVhost: true,
	})
	if err != nil {
		// Best-effort: vhost may already exist.
		fmt.Fprintf(w, "maxio: warning: create site maxio.test: %v\n", err)
	}

	fmt.Fprintln(w, "maxio: creating s3.maxio.test Caddy vhost (S3 API)...")
	_, err = m.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:       "s3.maxio.test",
		SiteType:     "ws",
		WSUpstream:   "127.0.0.1:9000",
		HTTPS:        true,
		ServiceVhost: true,
	})
	if err != nil {
		fmt.Fprintf(w, "maxio: warning: create site s3.maxio.test: %v\n", err)
	}

	// 9. Transfer ownership to the site user.
	if m.siteUser != "" {
		fmt.Fprintf(w, "maxio: chowning %s to %s...\n", maxioDir, m.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", m.siteUser, m.siteUser, maxioDir)
		if _, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("maxio: chown: %w", err)
		}
	}

	fmt.Fprintln(w, "maxio: install complete")
	return nil
}

func (m *MaxIOInstaller) Purge(ctx context.Context) error {
	return m.PurgeW(ctx, io.Discard, false)
}

func (m *MaxIOInstaller) PurgeW(ctx context.Context, w io.Writer, _ bool) error {
	// Stop the supervised process first.
	if err := m.supervisor.Stop("maxio"); err != nil {
		fmt.Fprintf(w, "maxio: warning: stop process: %v\n", err)
	}

	// Remove the Caddy vhosts and DB rows.
	if m.siteManager != nil {
		if err := m.siteManager.Delete(ctx, "maxio-test"); err != nil {
			fmt.Fprintf(w, "maxio: warning: delete site maxio.test: %v\n", err)
		}
		if err := m.siteManager.Delete(ctx, "s3-maxio-test"); err != nil {
			fmt.Fprintf(w, "maxio: warning: delete site s3.maxio.test: %v\n", err)
		}
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(m.serverRoot), "maxio")

	// Remove the service directory.
	maxioDir := paths.ServiceDir(m.serverRoot, "maxio")
	if err := os.RemoveAll(maxioDir); err != nil {
		return fmt.Errorf("maxio: remove dir: %w", err)
	}

	fmt.Fprintln(w, "maxio: purge complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest MaxIO version.
// If the context carries a pre-resolved version (via install.WithPreResolvedVersion),
// that value is returned immediately without hitting GitHub.
func (m *MaxIOInstaller) LatestVersion(ctx context.Context) (string, error) {
	if v := preResolvedVersionFromCtx(ctx); v != "" {
		return v, nil
	}
	tag, err := fetchGitHubLatestVersion(ctx, "coollabsio/maxio")
	if err != nil {
		return "", err
	}
	// Strip leading "v" to return a plain version string (e.g. "1.0.0").
	return strings.TrimPrefix(tag, "v"), nil
}

// UpdateW stops MaxIO, downloads and extracts the latest binary, and returns.
// The caller (API handler) restarts the process via the supervisor.
func (m *MaxIOInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := m.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("maxio: update: %w", err)
	}

	maxioDir := paths.ServiceDir(m.serverRoot, "maxio")
	binPath := filepath.Join(maxioDir, "maxio")

	// Stop the running process.
	fmt.Fprintln(w, "maxio: stopping...")
	if err := m.supervisor.Stop("maxio"); err != nil {
		fmt.Fprintf(w, "maxio: warning: stop: %v\n", err)
	}

	// Resolve the download URL for this version.
	_, dlURL, err := maxioLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("maxio: update: resolve download URL: %w", err)
	}

	// Download the new tarball to a temp location.
	tmpTar := filepath.Join(os.TempDir(), fmt.Sprintf("maxio-%s-update.tar.gz", latest))
	defer os.Remove(tmpTar)

	fmt.Fprintf(w, "maxio: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpTar); err != nil {
		return fmt.Errorf("maxio: update download: %w", err)
	}

	// Extract new binary to a temp path then rename into place.
	tmpBin := binPath + ".new"
	defer os.Remove(tmpBin)
	if err := extractFromTar(tmpTar, "maxio", tmpBin); err != nil {
		return fmt.Errorf("maxio: update extract: %w", err)
	}
	if err := os.Chmod(tmpBin, 0755); err != nil {
		return fmt.Errorf("maxio: chmod new binary: %w", err)
	}

	fmt.Fprintln(w, "maxio: replacing binary...")
	if err := os.Rename(tmpBin, binPath); err != nil {
		if copyErr := copyFile(tmpBin, binPath); copyErr != nil {
			return fmt.Errorf("maxio: replace binary: rename failed (%v), copy also failed: %w", err, copyErr)
		}
		if err := os.Chmod(binPath, 0755); err != nil {
			return fmt.Errorf("maxio: chmod binary after copy: %w", err)
		}
	}

	fmt.Fprintf(w, "maxio: binary replaced with %s\n", latest)
	return nil
}

// maxioLatestRelease queries the GitHub Releases API for coollabsio/maxio and
// returns the latest version string (without "v" prefix) and the browser
// download URL for the linux-amd64 tar.gz asset.
func maxioLatestRelease(ctx context.Context) (version, downloadURL string, err error) {
	apiURL := "https://api.github.com/repos/coollabsio/maxio/releases/latest"
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("maxio: github releases: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "devctl/1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("maxio: github releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("maxio: github releases: HTTP %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", fmt.Errorf("maxio: github releases: decode: %w", err)
	}
	if payload.TagName == "" {
		return "", "", fmt.Errorf("maxio: github releases: empty tag_name")
	}

	version = strings.TrimPrefix(payload.TagName, "v")

	// Find the linux-amd64 tar.gz asset.
	for _, asset := range payload.Assets {
		if strings.Contains(asset.Name, "linux-amd64") && strings.HasSuffix(asset.Name, ".tar.gz") {
			return version, asset.BrowserDownloadURL, nil
		}
	}

	return "", "", fmt.Errorf("maxio: github releases: no linux-amd64 tar.gz asset found in release %s", payload.TagName)
}

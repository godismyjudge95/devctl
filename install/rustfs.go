package install

import (
	"archive/zip"
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
	// rustfsZipURL always points to the latest Linux x86_64 musl binary release.
	rustfsZipURL = "https://dl.rustfs.com/artifacts/rustfs/release/rustfs-linux-x86_64-musl-latest.zip"
)

// RustFSInstaller downloads the RustFS binary to {serverRoot}/rustfs/,
// writes config.env with default credentials, creates the data directory, and
// registers a Caddy reverse-proxy vhost at rustfs.test for the console UI.
type RustFSInstaller struct {
	siteManager *sites.Manager
	supervisor  *services.Supervisor
	serverRoot  string
	siteUser    string
}

func (r *RustFSInstaller) ServiceID() string { return "rustfs" }

func (r *RustFSInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(r.serverRoot, "rustfs"), "rustfs"))
}

func (r *RustFSInstaller) Install(ctx context.Context) error {
	return r.InstallW(ctx, io.Discard)
}

func (r *RustFSInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if r.IsInstalled() {
		fmt.Fprintln(w, "rustfs: already installed")
		return nil
	}

	rustfsDir := paths.ServiceDir(r.serverRoot, "rustfs")
	binPath := filepath.Join(rustfsDir, "rustfs")
	dataDir := filepath.Join(rustfsDir, "data")
	envPath := filepath.Join(rustfsDir, "config.env")
	logsDir := paths.LogsDir(r.serverRoot)

	// 1. Create directories.
	fmt.Fprintln(w, "rustfs: creating directories...")
	for _, d := range []string{rustfsDir, dataDir, logsDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("rustfs: create dir %s: %w", d, err)
		}
	}

	// 2. Download zip archive (always latest).
	tmpZip := filepath.Join(os.TempDir(), "rustfs-latest.zip")
	defer os.Remove(tmpZip)

	fmt.Fprintln(w, "rustfs: downloading latest binary...")
	if err := curlDownloadW(ctx, w, rustfsZipURL, tmpZip); err != nil {
		return fmt.Errorf("rustfs: download: %w", err)
	}

	// 3. Extract the rustfs binary from the zip archive.
	fmt.Fprintln(w, "rustfs: extracting binary...")
	if err := extractRustFSBinary(tmpZip, binPath); err != nil {
		return fmt.Errorf("rustfs: extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("rustfs: chmod binary: %w", err)
	}

	// 4. Symlink into the shared bin dir so rustfs is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(r.serverRoot), "rustfs", binPath); err != nil {
		fmt.Fprintf(w, "rustfs: warning: %v\n", err)
	}

	// 5. Write config.env with credentials and configuration.
	fmt.Fprintln(w, "rustfs: writing config.env...")
	logPath := paths.LogPath(r.serverRoot, "rustfs")
	envContent := fmt.Sprintf(
		"RUSTFS_ACCESS_KEY=devctl\n"+
			"RUSTFS_SECRET_KEY=devctlsecret\n"+
			"RUSTFS_VOLUMES=%s\n"+
			"RUSTFS_ADDRESS=:9000\n"+
			"RUSTFS_CONSOLE_ADDRESS=:9001\n"+
			"RUSTFS_CONSOLE_ENABLE=true\n"+
			"RUST_LOG=error\n"+
			"RUSTFS_OBS_LOG_DIRECTORY=%s\n"+
			"RUSTFS_HOST=https://rustfs.test\n"+
			"RUSTFS_S3_HOST=https://s3.rustfs.test\n",
		dataDir,
		logsDir,
	)
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		return fmt.Errorf("rustfs: write config.env: %w", err)
	}

	// Write connection.env with Laravel-compatible AWS keys for easy copy-paste into .env.
	connPath := filepath.Join(rustfsDir, "connection.env")
	connContent := "AWS_ACCESS_KEY_ID=devctl\nAWS_SECRET_ACCESS_KEY=devctlsecret\nAWS_DEFAULT_REGION=us-east-1\nAWS_ENDPOINT=https://s3.rustfs.test\nAWS_USE_PATH_STYLE_ENDPOINT=true\n"
	if err := os.WriteFile(connPath, []byte(connContent), 0600); err != nil {
		return fmt.Errorf("rustfs: write connection.env: %w", err)
	}

	// Ensure log file exists.
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		f.Close()
	}

	// 6. Register Caddy reverse-proxy vhosts.
	fmt.Fprintln(w, "rustfs: creating rustfs.test Caddy vhost (console)...")
	_, err := r.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:     "rustfs.test",
		SiteType:   "ws",
		WSUpstream: "127.0.0.1:9001",
		HTTPS:      true,
	})
	if err != nil {
		// Best-effort: vhost may already exist.
		fmt.Fprintf(w, "rustfs: warning: create site rustfs.test: %v\n", err)
	}

	fmt.Fprintln(w, "rustfs: creating s3.rustfs.test Caddy vhost (S3 API)...")
	_, err = r.siteManager.Create(ctx, sites.CreateSiteInput{
		Domain:     "s3.rustfs.test",
		SiteType:   "ws",
		WSUpstream: "127.0.0.1:9000",
		HTTPS:      true,
	})
	if err != nil {
		fmt.Fprintf(w, "rustfs: warning: create site s3.rustfs.test: %v\n", err)
	}

	// 7. Transfer ownership to the site user.
	if r.siteUser != "" {
		fmt.Fprintf(w, "rustfs: chowning %s to %s...\n", rustfsDir, r.siteUser)
		chownCmd := fmt.Sprintf("chown -R %s:%s %s", r.siteUser, r.siteUser, rustfsDir)
		if _, err := runShellW(ctx, w, chownCmd); err != nil {
			return fmt.Errorf("rustfs: chown: %w", err)
		}
	}

	fmt.Fprintln(w, "rustfs: install complete")
	return nil
}

func (r *RustFSInstaller) Purge(ctx context.Context) error {
	return r.PurgeW(ctx, io.Discard, false)
}

func (r *RustFSInstaller) PurgeW(ctx context.Context, w io.Writer, _ bool) error {
	// Stop the supervised process first.
	if err := r.supervisor.Stop("rustfs"); err != nil {
		fmt.Fprintf(w, "rustfs: warning: stop process: %v\n", err)
	}

	// Remove the Caddy vhosts and DB rows.
	if r.siteManager != nil {
		if err := r.siteManager.Delete(ctx, "rustfs-test"); err != nil {
			fmt.Fprintf(w, "rustfs: warning: delete site rustfs.test: %v\n", err)
		}
		if err := r.siteManager.Delete(ctx, "s3-rustfs-test"); err != nil {
			fmt.Fprintf(w, "rustfs: warning: delete site s3.rustfs.test: %v\n", err)
		}
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(r.serverRoot), "rustfs")

	// Remove the service directory.
	rustfsDir := paths.ServiceDir(r.serverRoot, "rustfs")
	if err := os.RemoveAll(rustfsDir); err != nil {
		return fmt.Errorf("rustfs: remove dir: %w", err)
	}

	fmt.Fprintln(w, "rustfs: purge complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest RustFS version.
func (r *RustFSInstaller) LatestVersion(ctx context.Context) (string, error) {
	return fetchGitHubLatestVersion(ctx, "rustfs/rustfs")
}

// UpdateW stops RustFS, downloads and extracts the latest binary, and returns.
// The caller (API handler) restarts the process via the supervisor.
func (r *RustFSInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := r.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("rustfs: update: %w", err)
	}

	rustfsDir := paths.ServiceDir(r.serverRoot, "rustfs")
	binPath := filepath.Join(rustfsDir, "rustfs")

	// Stop the running process.
	fmt.Fprintln(w, "rustfs: stopping...")
	if err := r.supervisor.Stop("rustfs"); err != nil {
		fmt.Fprintf(w, "rustfs: warning: stop: %v\n", err)
	}

	// Download the new zip to a temp location.
	tmpZip := filepath.Join(os.TempDir(), "rustfs-update.zip")
	defer os.Remove(tmpZip)

	fmt.Fprintf(w, "rustfs: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, rustfsZipURL, tmpZip); err != nil {
		return fmt.Errorf("rustfs: update download: %w", err)
	}

	// Extract new binary to a temp path then rename into place.
	tmpBin := binPath + ".new"
	defer os.Remove(tmpBin)
	if err := extractRustFSBinary(tmpZip, tmpBin); err != nil {
		return fmt.Errorf("rustfs: update extract: %w", err)
	}
	if err := os.Chmod(tmpBin, 0755); err != nil {
		return fmt.Errorf("rustfs: chmod new binary: %w", err)
	}

	fmt.Fprintln(w, "rustfs: replacing binary...")
	if err := os.Rename(tmpBin, binPath); err != nil {
		if copyErr := copyFile(tmpBin, binPath); copyErr != nil {
			return fmt.Errorf("rustfs: replace binary: rename failed (%v), copy also failed: %w", err, copyErr)
		}
		if err := os.Chmod(binPath, 0755); err != nil {
			return fmt.Errorf("rustfs: chmod binary after copy: %w", err)
		}
	}

	fmt.Fprintf(w, "rustfs: binary replaced with %s\n", latest)
	return nil
}

// extractRustFSBinary opens the zip at zipPath, finds the "rustfs" entry
// (ignoring path prefixes), and writes it to dest.
func extractRustFSBinary(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		// Match any entry whose base name is "rustfs" (no extension).
		if filepath.Base(f.Name) == "rustfs" && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return fmt.Errorf("open zip entry %s: %w", f.Name, err)
			}
			defer rc.Close()

			out, err := os.Create(dest)
			if err != nil {
				return fmt.Errorf("create dest %s: %w", dest, err)
			}
			defer out.Close()

			if _, err := io.Copy(out, rc); err != nil {
				return fmt.Errorf("extract %s: %w", f.Name, err)
			}
			return nil
		}
	}
	return fmt.Errorf("rustfs binary not found in zip archive")
}

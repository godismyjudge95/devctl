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
)

const (
	caddyVersion = "v2.10.0"
	caddyURL     = "https://github.com/caddyserver/caddy/releases/download/" + caddyVersion + "/caddy_2.10.0_linux_amd64.tar.gz"
)

// CaddyInstaller downloads the Caddy binary to {serverRoot}/caddy/
// and runs it as a supervised child process.
type CaddyInstaller struct {
	supervisor *services.Supervisor
	serverRoot string // absolute path to the devctl server directory (e.g. "/home/alice/ddev/sites/server")
}

// NewCaddyInstaller creates a CaddyInstaller. It can be called before the full
// install registry is built — only supervisor and serverRoot are required.
func NewCaddyInstaller(supervisor *services.Supervisor, serverRoot string) *CaddyInstaller {
	return &CaddyInstaller{supervisor: supervisor, serverRoot: serverRoot}
}

func (c *CaddyInstaller) ServiceID() string { return "caddy" }

func (c *CaddyInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(c.serverRoot, "caddy"), "caddy"))
}

func (c *CaddyInstaller) Install(ctx context.Context) error {
	return c.InstallW(ctx, io.Discard)
}

func (c *CaddyInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if c.IsInstalled() {
		fmt.Fprintln(w, "caddy: already installed")
		return nil
	}

	caddyDir := paths.ServiceDir(c.serverRoot, "caddy")
	binPath := filepath.Join(caddyDir, "caddy")
	tmpTar := filepath.Join(os.TempDir(), "caddy-linux-amd64.tar.gz")
	defer os.Remove(tmpTar)

	// 1. Create directory (data/ subdir for autosave.json and internal CA certs).
	fmt.Fprintln(w, "caddy: creating directory...")
	if err := os.MkdirAll(filepath.Join(caddyDir, "data"), 0755); err != nil {
		return fmt.Errorf("caddy: create dir: %w", err)
	}

	// 2. Download tarball.
	fmt.Fprintf(w, "caddy: downloading %s...\n", caddyVersion)
	if err := curlDownloadW(ctx, w, caddyURL, tmpTar); err != nil {
		return fmt.Errorf("caddy: download: %w", err)
	}

	// 3. Extract caddy binary (binary is at root of tarball).
	fmt.Fprintln(w, "caddy: extracting binary...")
	if err := extractFromTar(tmpTar, "caddy", binPath); err != nil {
		return fmt.Errorf("caddy: extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("caddy: chmod binary: %w", err)
	}

	// 4. Symlink into the shared bin dir so caddy is in PATH.
	if err := LinkIntoBinDir(paths.BinDir(c.serverRoot), "caddy", binPath); err != nil {
		fmt.Fprintf(w, "caddy: warning: %v\n", err)
	}

	// 5. Write env file so the supervisor sets HOME to caddyDir,
	//    which redirects Caddy's autosave.json and internal CA data
	//    to a path devctl controls.
	envPath := filepath.Join(caddyDir, "caddy.env")
	if err := os.WriteFile(envPath, []byte("HOME="+caddyDir+"\n"), 0600); err != nil {
		return fmt.Errorf("caddy: write caddy.env: %w", err)
	}

	fmt.Fprintln(w, "caddy: install complete")
	return nil
}

func (c *CaddyInstaller) Purge(ctx context.Context) error {
	return c.PurgeW(ctx, io.Discard, false)
}

func (c *CaddyInstaller) PurgeW(ctx context.Context, w io.Writer, _ bool) error {
	// Stop the supervised process first.
	if err := c.supervisor.Stop("caddy"); err != nil {
		fmt.Fprintf(w, "caddy: warning: stop process: %v\n", err)
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(c.serverRoot), "caddy")

	// Remove the directory (binary + data).
	caddyDir := paths.ServiceDir(c.serverRoot, "caddy")
	if err := os.RemoveAll(caddyDir); err != nil {
		return fmt.Errorf("caddy: remove dir: %w", err)
	}

	fmt.Fprintln(w, "caddy: purge complete")
	return nil
}

// LatestVersion queries GitHub Releases for the latest Caddy version.
func (c *CaddyInstaller) LatestVersion(ctx context.Context) (string, error) {
	return fetchGitHubLatestVersion(ctx, "caddyserver/caddy")
}

// UpdateW stops Caddy and replaces the binary with the latest version.
// The caller (API handler) is responsible for restarting the service.
func (c *CaddyInstaller) UpdateW(ctx context.Context, w io.Writer) error {
	latest, err := c.LatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("caddy: update: %w", err)
	}
	// Strip leading "v" for the tarball filename (e.g. "v2.10.0" → "2.10.0").
	ver := strings.TrimPrefix(latest, "v")
	dlURL := fmt.Sprintf("https://github.com/caddyserver/caddy/releases/download/%s/caddy_%s_linux_amd64.tar.gz", latest, ver)

	caddyDir := paths.ServiceDir(c.serverRoot, "caddy")
	binPath := filepath.Join(caddyDir, "caddy")
	tmpTar := filepath.Join(os.TempDir(), "caddy-update-linux-amd64.tar.gz")
	defer os.Remove(tmpTar)

	fmt.Fprintf(w, "caddy: downloading %s...\n", latest)
	if err := curlDownloadW(ctx, w, dlURL, tmpTar); err != nil {
		return fmt.Errorf("caddy: update download: %w", err)
	}

	fmt.Fprintln(w, "caddy: stopping caddy...")
	if err := c.supervisor.Stop("caddy"); err != nil {
		fmt.Fprintf(w, "caddy: warning: stop: %v\n", err)
	}

	fmt.Fprintln(w, "caddy: replacing binary...")
	if err := extractFromTar(tmpTar, "caddy", binPath); err != nil {
		return fmt.Errorf("caddy: update extract: %w", err)
	}
	if err := os.Chmod(binPath, 0755); err != nil {
		return fmt.Errorf("caddy: update chmod: %w", err)
	}

	fmt.Fprintf(w, "caddy: binary replaced with %s\n", latest)
	return nil
}

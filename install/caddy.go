package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/danielgormly/devctl/paths"
	"github.com/danielgormly/devctl/services"
)

const (
	caddyVersion = "v2.10.0"
	caddyURL     = "https://github.com/caddyserver/caddy/releases/download/" + caddyVersion + "/caddy_2.10.0_linux_amd64.tar.gz"
)

// CaddyInstaller downloads the Caddy binary to $HOME/sites/server/caddy/
// and runs it as a supervised child process.
type CaddyInstaller struct {
	supervisor *services.Supervisor
	siteHome   string // home directory of the non-root site user (e.g. "/home/alice")
}

// NewCaddyInstaller creates a CaddyInstaller. It can be called before the full
// install registry is built — only supervisor and siteHome are required.
func NewCaddyInstaller(supervisor *services.Supervisor, siteHome string) *CaddyInstaller {
	return &CaddyInstaller{supervisor: supervisor, siteHome: siteHome}
}

func (c *CaddyInstaller) ServiceID() string { return "caddy" }

func (c *CaddyInstaller) IsInstalled() bool {
	return fileExists(filepath.Join(paths.ServiceDir(c.siteHome, "caddy"), "caddy"))
}

func (c *CaddyInstaller) Install(ctx context.Context) error {
	return c.InstallW(ctx, io.Discard)
}

func (c *CaddyInstaller) InstallW(ctx context.Context, w io.Writer) error {
	if c.IsInstalled() {
		fmt.Fprintln(w, "caddy: already installed")
		return nil
	}

	caddyDir := paths.ServiceDir(c.siteHome, "caddy")
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
	if err := LinkIntoBinDir(paths.BinDir(c.siteHome), "caddy", binPath); err != nil {
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
	return c.PurgeW(ctx, io.Discard)
}

func (c *CaddyInstaller) PurgeW(ctx context.Context, w io.Writer) error {
	// Stop the supervised process first.
	if err := c.supervisor.Stop("caddy"); err != nil {
		fmt.Fprintf(w, "caddy: warning: stop process: %v\n", err)
	}

	// Remove bin dir symlink.
	UnlinkFromBinDir(paths.BinDir(c.siteHome), "caddy")

	// Remove the directory (binary + data).
	caddyDir := paths.ServiceDir(c.siteHome, "caddy")
	if err := os.RemoveAll(caddyDir); err != nil {
		return fmt.Errorf("caddy: remove dir: %w", err)
	}

	fmt.Fprintln(w, "caddy: purge complete")
	return nil
}
